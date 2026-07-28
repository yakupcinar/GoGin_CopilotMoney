package middleware

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// Rate limiting — iki ayrı ihtiyaç var:
//
//	Public auth endpoint'leri (/login, /register, /auth/refresh):
//	  IP başına sınır. Amaç brute-force'u pahalı kılmak. bcrypt'in yavaşlığı
//	  kısmi koruma sağlıyor ama tek başına yeterli değil.
//
//	/chat:
//	  KULLANICI başına sınır. Buradaki maliyet CPU değil PARA — her istek
//	  gerçek bir LLM çağrısı. Tek kullanıcı döngüye soksa hem API kotasını
//	  hem faturayı tüketir.
//
// SINIR: bellekte tutuluyor. Tek sunucu için doğru; birden fazla kopya
// çalıştırılırsa her kopyanın kendi sayacı olur ve efektif limit kopya
// sayısıyla çarpılır. O noktada Redis gibi paylaşımlı bir sayaç gerekir.

// Limiter — hız sınırlamanın DEĞİŞEN kısmı: "bu anahtar için bir istek
// harcanabilir mi?" Sayacın nerede tutulduğu (bellek / Redis) yalnızca burayı
// etkiler.
//
// NEDEN SADECE TEK METOT: Limit() HTTP cevabı yazar, depolamayla ilgisi yoktur
// ve her implementasyonda kopyalanmamalı; Sweep/StartSweeper ise yalnızca
// bellek implementasyonuna aittir (Redis'te TTL o işi yapar). Interface,
// değişen davranış kadar küçük olmalı — büyüdükçe soyutlama zayıflar.
type Limiter interface {
	Allow(ctx context.Context, key string) bool
}

// PlanAwareLimiter — abonelik planına göre EŞİĞİ ÇAĞIRAN TARAF belirlediğinde
// gereken ikinci, küçük arayüz.
//
// NEDEN AYRI (Limiter'a eklenmedi): /login ve /register için eşik sabit
// (AUTH_RATE_PER_MIN) — herkese aynı, planla ilgisi yok. /chat için eşik
// KULLANICININ PLANINA göre değişir (free/pro). Limiter'ın imzasına perMinute
// eklemek, plana hiç ihtiyacı olmayan auth limiter'ı da gereksiz yere
// değiştirirdi. Interface, değişen davranış kadar küçük olmalı (Bölüm 28).
type PlanAwareLimiter interface {
	AllowWithLimit(ctx context.Context, key string, perMinute int) bool
}

// FullLimiter — her iki arayüzü de karşılayan implementasyonlar için.
// Yalnızca constructor dönüş tiplerinde kullanılır; çağıran taraf ihtiyacına
// göre Limiter ya da PlanAwareLimiter olarak daraltır.
type FullLimiter interface {
	Limiter
	PlanAwareLimiter
}

// Derleme zamanı kontrolleri.
var (
	_ Limiter          = (*RateLimiter)(nil)
	_ PlanAwareLimiter = (*RateLimiter)(nil)
)

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type RateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	limit    rate.Limit
	burst    int
	// ttl — bu süre boyunca görülmeyen anahtarlar silinir.
	// Olmasaydı map her yeni IP ile büyürdü: bellek tüketme saldırısına açık.
	ttl time.Duration
}

// NewRateLimiter — perMinute: dakikada izin verilen istek, burst: anlık patlama payı.
func NewRateLimiter(perMinute int, burst int) *RateLimiter {
	if perMinute <= 0 {
		perMinute = 60
	}
	if burst <= 0 {
		burst = 1
	}
	return &RateLimiter{
		visitors: make(map[string]*visitor),
		limit:    rate.Every(time.Minute / time.Duration(perMinute)),
		burst:    burst,
		ttl:      10 * time.Minute,
	}
}

// Allow — anahtar için bir istek harcamayı dener, kurucudaki sabit limitle.
//
// ctx kullanılmıyor: sayaç bellekte, bloklama yok. İmzada olmasının sebebi
// başka implementasyonların (Redis) ağ üzerinden gitmesi.
func (rl *RateLimiter) Allow(_ context.Context, key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	v, ok := rl.visitors[key]
	if !ok {
		v = &visitor{limiter: rate.NewLimiter(rl.limit, rl.burst)}
		rl.visitors[key] = v
	}
	v.lastSeen = time.Now()
	return v.limiter.Allow()
}

// AllowWithLimit — Allow ile aynı, ama eşik ÇAĞIRAN TARAFTAN gelir (plana
// göre değişir), kurucudaki sabit rl.limit'ten değil.
//
// Aynı anahtarın rate.Limiter'ı yeniden kullanılır (SetLimit ucuzdur, yeni
// oran hemen uygulanır) — kullanıcı planını yükseltirse bir sonraki istekte
// yeni hız devreye girer, eski bucket'ı atıp yeniden oluşturmaya gerek yok.
func (rl *RateLimiter) AllowWithLimit(_ context.Context, key string, perMinute int) bool {
	if perMinute <= 0 {
		perMinute = 1
	}
	limit := rate.Every(time.Minute / time.Duration(perMinute))

	rl.mu.Lock()
	defer rl.mu.Unlock()

	v, ok := rl.visitors[key]
	if !ok {
		v = &visitor{limiter: rate.NewLimiter(limit, rl.burst)}
		rl.visitors[key] = v
	} else {
		v.limiter.SetLimit(limit)
	}
	v.lastSeen = time.Now()
	return v.limiter.Allow()
}

// Sweep — uzun süredir görülmeyen anahtarları siler. Periyodik çağrılmalı.
func (rl *RateLimiter) Sweep(now time.Time) int {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	removed := 0
	for key, v := range rl.visitors {
		if now.Sub(v.lastSeen) > rl.ttl {
			delete(rl.visitors, key)
			removed++
		}
	}
	return removed
}

// StartSweeper — ctx iptal edilene kadar periyodik temizlik yapar.
func (rl *RateLimiter) StartSweeper(stop <-chan struct{}) {
	ticker := time.NewTicker(rl.ttl)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case now := <-ticker.C:
			rl.Sweep(now)
		}
	}
}

// KeyByIP — public endpoint'ler için. Kimlik yok, elimizdeki tek şey IP.
func KeyByIP(c *gin.Context) string { return "ip:" + c.ClientIP() }

// KeyByUser — korumalı endpoint'ler için. AuthMiddleware'den SONRA gelmeli.
// IP yerine kullanıcı: aynı ofisten çalışan kullanıcılar birbirini
// engellemesin, ama tek kullanıcı çok istek atarsa yalnızca kendisi kısıtlansın.
func KeyByUser(c *gin.Context) string {
	if v, ok := c.Get("user_id"); ok {
		if id, ok := v.(int); ok {
			return "user:" + strconv.Itoa(id)
		}
	}
	return "ip:" + c.ClientIP() // güvenli geri düşüş
}

// Limit — verilen Limiter'ı kullanan gin middleware'i üretir.
//
// Metot değil PAKET FONKSİYONU: HTTP cevabı (429 + Retry-After + Abort) her
// implementasyon için AYNI. Interface'e koysaydık her sayaç bu mantığı yeniden
// yazmak ve gin'i import etmek zorunda kalırdı.
func Limit(l Limiter, keyFn func(*gin.Context) string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !l.Allow(c.Request.Context(), keyFn(c)) {
			tooManyRequests(c)
			return
		}
		c.Next()
	}
}

// tooManyRequests — Limit ve LimitByPlan'ın paylaştığı 429 cevabı.
func tooManyRequests(c *gin.Context) {
	// Retry-After: istemciye ne zaman tekrar deneyeceğini söyle.
	// Olmadan istemciler agresif biçimde yeniden dener ve durumu kötüleştirir.
	c.Header("Retry-After", "60")
	c.JSON(http.StatusTooManyRequests, gin.H{
		"error": "Too many requests, please slow down"})
	c.Abort()
}

// LimitByPlan — abonelik planına göre değişen eşikli gin middleware'i üretir.
//
// planFn HER İSTEKTE çağrılır (taze okuma) — kullanıcının planı yükseltilir
// yükseltilmez yeni limit uygulanır, token yenilenmesini beklemez. Bunun
// bedeli: her /chat isteği bir kullanıcı sorgusu daha yapar. Kabul edilebilir,
// çünkü chat isteği zaten kategori/hesap için veritabanına gidiyor.
//
// planFn hata dönerse ya da plan haritada yoksa defaultLimit kullanılır.
// defaultLimit EN KISITLAYICI (free) katman olmalı: bir veritabanı arızasında
// "cömert davran" değil "tutucu davran" — bu limitin amacı maliyeti korumak,
// arıza anında maliyeti serbest bırakmak yanlış taraf olurdu.
func LimitByPlan(l PlanAwareLimiter, keyFn func(*gin.Context) string,
	planFn func(*gin.Context) (string, error), limits map[string]int, defaultLimit int) gin.HandlerFunc {

	return func(c *gin.Context) {
		limit := defaultLimit
		if plan, err := planFn(c); err == nil {
			if v, ok := limits[plan]; ok {
				limit = v
			}
		}

		if !l.AllowWithLimit(c.Request.Context(), keyFn(c), limit) {
			tooManyRequests(c)
			return
		}
		c.Next()
	}
}
