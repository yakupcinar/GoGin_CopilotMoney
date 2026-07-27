package middleware

import (
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
	Allow(key string) bool
}

// Derleme zamanı kontrolü.
var _ Limiter = (*RateLimiter)(nil)

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

// Allow — anahtar için bir istek harcamayı dener.
func (rl *RateLimiter) Allow(key string) bool {
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
		if !l.Allow(keyFn(c)) {
			// Retry-After: istemciye ne zaman tekrar deneyeceğini söyle.
			// Olmadan istemciler agresif biçimde yeniden dener ve durumu kötüleştirir.
			c.Header("Retry-After", "60")
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "Too many requests, please slow down"})
			c.Abort()
			return
		}
		c.Next()
	}
}
