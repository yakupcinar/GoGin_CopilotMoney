package repositories

import (
	"GoGinMoneyCopilot/models"
	"context"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// DefaultWarmUpInterval — StartWarmUpLoop için varsayılan. ACCESS_TOKEN_TTL'den
// (varsayılan 15dk) kısa tutuluyor ki Redis kendi başına restart olduğunda
// "nöbetçi yok -> Postgres'e düş" penceresi kısa kalsın.
const DefaultWarmUpInterval = 5 * time.Minute

// Access token denylist'inin Redis kopyası.
//
// TASARIM: Redis burada bir CACHE değil, kaynağın TAM KOPYASI. Fark önemli —
// cache'te "anahtar yok" belirsizdir ("iptal edilmemiş" mi, "henüz
// cache'lenmemiş" mi?), tam kopyada ise KESİN olarak "iptal edilmemiş"
// demektir. Bu ancak küme eksiksizse doğrudur; aşağıdaki nöbetçi anahtar
// tam olarak bunu güvence altına alır.
//
// Kalıcı kaynak Postgres'tir (source). Redis'e güvenilemediği HER durumda
// oraya düşülür: yavaş ama doğru.
//
// Bu bir "decorator": TokenRepository'yi karşılar ve içine yine bir
// TokenRepository alır. main.go'da zincir gorm -> redis şeklinde kurulur.

const (
	denylistPrefix = "jti:"

	// denylistWarmKey — NÖBETÇİ ANAHTAR.
	//
	// Redis bellekte çalışıyor ve kalıcılığı bilerek kapalı; yeniden başlarsa
	// tüm veri gider. O anda Redis ÇALIŞIYOR ve sorunsuz "anahtar yok" cevabı
	// verir — yani hataya bakarak bunu anlayamayız. Nöbetçi bunu görünür kılar:
	// warm-up'ın sonunda yazılır, kaybolduysa kopya eksiktir.
	//
	// (Redis'in "Sentinel" adlı yüksek-erişilebilirlik ürünüyle ilgisi yoktur.)
	denylistWarmKey = "denylist:warm"

	// denylistLockKey — LİDER KİLİDİ. Çok kopyalı dağıtımda her kopya kendi
	// StartWarmUpLoop goroutine'ini bağımsız çalıştırır; kilit olmadan N
	// kopya her turda AYNI Postgres sorgusunu N kez atar ve Redis'e aynı
	// veriyi N kez yazar — yanlış değil, sadece israf.
	denylistLockKey = "denylist:warmup:lock"
)

type redisTokenRepository struct {
	rdb    *redis.Client
	source TokenRepository // kalıcı kaynak (gorm)
}

func NewRedisTokenRepository(rdb *redis.Client, source TokenRepository) TokenRepository {
	return &redisTokenRepository{rdb: rdb, source: source}
}

func denylistKey(jti string) string { return denylistPrefix + jti }

// Revoke — önce kalıcı kaynağa, sonra kopyaya.
//
// SIRA ÖNEMLİ: Postgres'e yazılamadıysa iptal GERÇEKLEŞMEMİŞTİR, hata döner.
// Redis'e yazılamadıysa iptal gerçekleşmiştir ama kopya eksik kalır — o zaman
// nöbetçiyi düşürüp okumaları Postgres'e yönlendiriyoruz.
func (r *redisTokenRepository) Revoke(ctx context.Context, jti string, expiresAt time.Time) error {
	if err := r.source.Revoke(ctx, jti, expiresAt); err != nil {
		return err
	}

	// TTL = token'ın kalan ömrü. Süresi zaten dolmuşsa Redis'e yazmaya gerek
	// yok: JWT doğrulaması exp'e bakıp onu zaten reddeder.
	ttl := time.Until(expiresAt)
	if ttl <= 0 {
		return nil
	}

	if err := r.rdb.Set(ctx, denylistKey(jti), 1, ttl).Err(); err != nil {
		// Kopya artık EKSİK. Nöbetçiyi silerek IsRevoked'ı Postgres'e
		// yönlendiriyoruz. Kullanıcıya hata DÖNMÜYORUZ çünkü iptal
		// gerçekten oldu (Postgres'e yazıldı) ve okumalar doğru cevap verecek.
		r.rdb.Del(ctx, denylistWarmKey)
		log.Println("denylist: redis write failed, falling back to postgres:", err)
	}
	return nil
}

// IsRevoked — nöbetçi ve token TEK gidiş-dönüşte okunur.
//
// NEDEN TEK KOMUT: ayrı iki sorgu olsaydı aralarında Redis sıfırlanabilir ve
// "nöbetçi vardı" bilgisiyle eksik bir kümeden cevap verilebilirdi.
//
// Redis'e güvenilemeyen HER durum (erişilemiyor / nöbetçi yok) aynı yere
// çıkar: kaynağa sor. Doğruluk hiçbir koşulda bozulmaz, yalnızca hız düşer.
func (r *redisTokenRepository) IsRevoked(ctx context.Context, jti string) (bool, error) {
	vals, err := r.rdb.MGet(ctx, denylistWarmKey, denylistKey(jti)).Result()
	if err != nil || len(vals) != 2 || vals[0] == nil {
		return r.source.IsRevoked(ctx, jti)
	}
	// Nöbetçi yerinde -> kopya tam -> yokluk KESİN olarak "iptal edilmemiş".
	return vals[1] != nil, nil
}

// DeleteExpired — Redis'te yapacak bir şey yok, TTL zaten siliyor.
// Bu metodun "yarı boş" kalması, Redis'e taşımanın somut kazancıdır.
func (r *redisTokenRepository) DeleteExpired(ctx context.Context, before time.Time) (int64, error) {
	return r.source.DeleteExpired(ctx, before)
}

// ListActive — yalnızca warm-up içindir, kaynağa devredilir.
func (r *redisTokenRepository) ListActive(ctx context.Context, now time.Time) ([]models.RevokedToken, error) {
	return r.source.ListActive(ctx, now)
}

// WarmUpDenylist — Postgres'teki süresi dolmamış iptal kayıtlarını Redis'e
// basar ve nöbetçi anahtarı yazar. Açılışta bir kez çağrılır.
//
// Bu adım OLMADAN tasarımın dayandığı varsayım ("Redis'te yoksa iptal
// edilmemiştir") yanlış olur: boş bir Redis, iptal edilmiş her token'ı
// geçerli gösterirdi.
//
// Başarısız olursa uygulama yine de çalışır — nöbetçi yazılmadığı için
// IsRevoked Postgres'e düşer. Yavaş ama doğru; o yüzden ölümcül değil.
func WarmUpDenylist(ctx context.Context, rdb *redis.Client, source TokenRepository, now time.Time) (int, error) {
	tokens, err := source.ListActive(ctx, now)
	if err != nil {
		return 0, err
	}

	// Pipeline: N ayrı gidiş-dönüş yerine tek seferde gönder.
	pipe := rdb.Pipeline()
	loaded := 0
	for _, t := range tokens {
		ttl := t.ExpiresAt.Sub(now)
		if ttl <= 0 {
			continue
		}
		pipe.Set(ctx, denylistKey(t.JTI), 1, ttl)
		loaded++
	}
	// Nöbetçi EN SON: yarıda kalırsa kopya eksik sayılır ve okumalar
	// Postgres'e düşer. TTL yok — kopya geçerli olduğu sürece durmalı.
	pipe.Set(ctx, denylistWarmKey, 1, 0)

	if _, err := pipe.Exec(ctx); err != nil {
		return 0, fmt.Errorf("denylist warm-up failed: %w", err)
	}
	return loaded, nil
}

// StartWarmUpLoop — WarmUpDenylist'i periyodik olarak tekrar çalıştırır.
//
// NEDEN GEREKLİ: nöbetçi yalnızca UYGULAMA AÇILIŞINDA yazılıyordu. Redis
// uygulama AYAKTAYKEN kendi başına restart olursa (bakım, bellek yetersizliği)
// veri gider ama uygulama bunu asla öğrenmez — nöbetçi bir daha yazılmadığı
// için okumalar SÜRESİZ olarak Postgres'e düşer. Bu döngü, düşme süresini
// interval ile sınırlıyor: en kötü ihtimalle bir sonraki tur kadar bekler.
//
// interval <= 0 verilirse DefaultWarmUpInterval kullanılır.
// stop kapanınca döngü sonlanır (Cleaner.Start ve RateLimiter.StartSweeper
// ile aynı desen).
func StartWarmUpLoop(rdb *redis.Client, source TokenRepository, interval time.Duration, stop <-chan struct{}) {
	if interval <= 0 {
		interval = DefaultWarmUpInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			runWarmUpIfLeader(ctx, rdb, source, interval, time.Now())
			cancel()
		}
	}
}

// runWarmUpIfLeader — SET NX ile YALNIZCA BİR kopyanın bu turu çalıştırmasını
// sağlar. Kilidi alamayan kopyalar sessizce atlar; bu bir hata değil, "başka
// biri zaten yapıyor" demek.
//
// Kilidin TTL'i interval kadar: kilidi alan kopya çökse bile bir sonraki
// turu başka bir kopya devralır. Kimin aldığı önemli değil, yalnızca birinin
// alması önemli.
func runWarmUpIfLeader(ctx context.Context, rdb *redis.Client, source TokenRepository, interval time.Duration, now time.Time) {
	acquired, err := rdb.SetNX(ctx, denylistLockKey, 1, interval).Result()
	if err != nil {
		// Kilit kontrolü başarısız: riske girmeden atla. Bir sonraki tur
		// yine dener; tek bir kaçırılmış tur ölümcül değil.
		log.Println("denylist warm-up: lock check failed, skipping this turn:", err)
		return
	}
	if !acquired {
		return // başka bir kopya bu turu üstlendi
	}

	if _, err := WarmUpDenylist(ctx, rdb, source, now); err != nil {
		log.Println("denylist warm-up (periodic) failed:", err)
	}
}
