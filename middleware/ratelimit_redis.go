package middleware

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Rate limiting'in Redis'li implementasyonu.
//
// NEDEN GEREKLİ: bellekteki sayaç SÜREÇ İÇİNDEDİR. Birden fazla kopya
// çalıştığında her kopyanın kendi sayacı olur ve efektif limit kopya sayısıyla
// çarpılır. Ölçülen: limit 10/dk iken 2 kopyada 14 istekten 9'u geçti (~2 katı).
// Redis sayacı konteynerlerin DIŞINDA, ortak bir yerde tutar.
//
// SABİT PENCERE (fixed window): anahtar dakika numarasını içerir, her dakika
// yeni bir anahtar oluşur ve eskisi TTL ile kendiliğinden silinir — Sweep'e
// gerek yok. Bilinen zaafı pencere sınırı: 59. saniyede N, 61. saniyede N daha
// geçebilir. Daha hassas alternatifler (sliding window) bu ölçekte gereksiz.

type redisLimiter struct {
	rdb       *redis.Client
	namespace string // authLimiter ve chatLimiter aynı Redis'i paylaşıyor
	perMinute int

	// fallback — Redis erişilemediğinde kullanılan süreç-içi sayaç.
	//
	// NEDEN FAIL-OPEN DEĞİL: rate limiting için sektörün varsayılanı hata
	// durumunda izin vermektir (koruma mekanizması kesintinin sebebi olmasın).
	// Ama burada limitler ya PARA (/chat -> LLM çağrısı) ya CPU (/login ->
	// bcrypt ~100ms) koruyor; ikisini birden sıfırlamak kabul edilemez.
	// Yedek kaba bir koruma verir (kopyalar arası paylaşımsız) — tam korumadan
	// az, sıfır korumadan çok.
	fallback Limiter

	// warnOnce — Redis düştüğünde log'u her istekte tekrarlamamak için.
	warnOnce sync.Once
}

func NewRedisLimiter(rdb *redis.Client, namespace string, perMinute int, fallback Limiter) Limiter {
	if perMinute <= 0 {
		perMinute = 60
	}
	return &redisLimiter{
		rdb:       rdb,
		namespace: namespace,
		perMinute: perMinute,
		fallback:  fallback,
	}
}

// Derleme zamanı kontrolü.
var _ Limiter = (*redisLimiter)(nil)

func (l *redisLimiter) Allow(ctx context.Context, key string) bool {
	// Kısa zaman aşımı: Redis takılırsa her istek onu beklemesin.
	// İsteğin kendi context'inden türetiliyor, yani istemci koparsa bu da düşer.
	ctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()

	window := time.Now().Unix() / 60
	k := fmt.Sprintf("rl:%s:%s:%d", l.namespace, key, window)

	// INCR + EXPIRE tek gidiş-dönüşte.
	//
	// EXPIRE her artışta yeniden veriliyor. "Yalnızca ilk artışta ver" daha
	// tasarruflu olurdu ama süreç aradan çekilirse anahtar TTL'siz kalır ve
	// sonsuza kadar yaşar. Anahtar zaten dakika numarası içerdiği için pencere
	// geçince yazılmayı bırakıyor; TTL'i tekrar vermek zararsız.
	// (Tam atomiklik isteyen bir Lua script'i de yazılabilirdi; bu ölçekte
	// gereksiz karmaşıklık.)
	pipe := l.rdb.Pipeline()
	incr := pipe.Incr(ctx, k)
	pipe.Expire(ctx, k, 70*time.Second)

	if _, err := pipe.Exec(ctx); err != nil {
		l.warnOnce.Do(func() {
			log.Println("ratelimit: redis unavailable, falling back to in-process counter:", err)
		})
		return l.fallback.Allow(ctx, key)
	}

	return incr.Val() <= int64(l.perMinute)
}
