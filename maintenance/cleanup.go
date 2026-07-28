// Package maintenance — periyodik bakım işleri.
//
// Şu an tek iş var: süresi geçmiş kayıtları silmek. Üç tablo da her
// kullanımda satır biriktiriyor ve hiçbiri kendi kendini temizlemiyordu:
//
//	revoked_tokens  — her logout bir satır
//	pending_actions — her silme onayı bir satır
//	refresh_tokens  — her login ve her yenileme bir satır
//
// Süresi dolmuş kayıtlar sorgu SONUÇLARINI etkilemiyor (hepsinde
// expires_at filtresi var), o yüzden bu bir doğruluk sorunu değil.
// Ama disk şişer ve indeksler yavaşlar — aylar içinde fark edilir.
package maintenance

import (
	"GoGinMoneyCopilot/repositories"
	"context"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// cleanerLockKey — LİDER KİLİDİ, çok kopyalı dağıtım için.
//
// NEDEN GEREKLİ: her app kopyası kendi Cleaner.Start goroutine'ini bağımsız
// çalıştırır. Kilit olmadan N kopya her saat AYNI üç DELETE sorgusunu N kez
// atar — yanlış değil, sadece israf. Aynı desen repositories.StartWarmUpLoop
// için de kullanıldı (SET NX ile "yoksa yaz").
const cleanerLockKey = "cleaner:lock"

// DefaultInterval — saatte bir yeterli. Bu iş aceleye gelmez;
// amaç tabloyu sınırlı tutmak, anında temizlemek değil.
const DefaultInterval = time.Hour

type Cleaner struct {
	tokens   repositories.TokenRepository
	pending  repositories.PendingActionRepository
	refresh  repositories.RefreshTokenRepository
	interval time.Duration

	// rdb — nil olabilir. Redis yoksa (REDIS_ADDR ayarlanmamış) kilit
	// atlanır, Cleaner eskisi gibi koşulsuz çalışır: tek kopya varsayımı,
	// israf riski yok.
	rdb *redis.Client
}

func NewCleaner(
	tokens repositories.TokenRepository,
	pending repositories.PendingActionRepository,
	refresh repositories.RefreshTokenRepository,
	interval time.Duration,
) *Cleaner {
	if interval <= 0 {
		interval = DefaultInterval
	}
	return &Cleaner{tokens: tokens, pending: pending, refresh: refresh, interval: interval}
}

// UseRedisLock — çok kopyalı dağıtımda yalnızca bir kopyanın temizlik
// yapmasını sağlar. rdb nil verilirse (Redis devre dışıysa) hiçbir şey
// değişmez — NewCleaner'ın imzasını bozmamak için ayrı, isteğe bağlı bir
// adım olarak tasarlandı.
func (c *Cleaner) UseRedisLock(rdb *redis.Client) *Cleaner {
	c.rdb = rdb
	return c
}

// Report — bir temizlik turunun sonucu. Test edilebilirlik ve loglama için.
type Report struct {
	RevokedTokens  int64
	PendingActions int64
	RefreshTokens  int64
}

func (r Report) Total() int64 {
	return r.RevokedTokens + r.PendingActions + r.RefreshTokens
}

// RunOnce — tek tur temizlik.
//
// Bir tablo hata verirse DİĞERLERİNE DEVAM EDER. Bakım işi kısmi başarıyla
// da değerlidir; hepsini birden iptal etmenin faydası yok.
func (c *Cleaner) RunOnce(ctx context.Context, now time.Time) Report {
	var rep Report

	if n, err := c.tokens.DeleteExpired(ctx, now); err != nil {
		log.Println("cleanup: revoked_tokens:", err)
	} else {
		rep.RevokedTokens = n
	}

	if n, err := c.pending.DeleteExpired(ctx, now); err != nil {
		log.Println("cleanup: pending_actions:", err)
	} else {
		rep.PendingActions = n
	}

	if n, err := c.refresh.DeleteExpired(ctx, now); err != nil {
		log.Println("cleanup: refresh_tokens:", err)
	} else {
		rep.RefreshTokens = n
	}

	return rep
}

// Start — ctx iptal edilene kadar periyodik olarak çalışır.
//
// Bir kez HEMEN çalışır: sunucu kapalıyken biriken kayıtlar ilk turu
// beklemesin. Goroutine olarak çağrıldığı için başlangıcı yavaşlatmaz.
//
// time.Sleep yerine ticker + select kullanıyoruz: kapanma sinyali gelince
// bir saat beklemeden çıkabilelim.
func (c *Cleaner) Start(ctx context.Context) {
	c.logRun(c.runIfLeader(ctx, time.Now()))

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("cleanup worker stopped")
			return
		case <-ticker.C:
			c.logRun(c.runIfLeader(ctx, time.Now()))
		}
	}
}

// runIfLeader — Redis varsa SET NX ile kilidi almayı dener, yoksa (rdb nil)
// koşulsuz çalışır. Kilidin TTL'i c.interval kadar: kilidi alan kopya çökse
// bile bir sonraki tur başka bir kopya devralır. Kimin aldığı önemli değil,
// yalnızca birinin alması önemli — repositories.StartWarmUpLoop'taki
// runWarmUpIfLeader ile aynı gerekçe.
func (c *Cleaner) runIfLeader(ctx context.Context, now time.Time) Report {
	if c.rdb == nil {
		return c.RunOnce(ctx, now)
	}

	acquired, err := c.rdb.SetNX(ctx, cleanerLockKey, 1, c.interval).Result()
	if err != nil {
		log.Println("cleanup: lock check failed, skipping this turn:", err)
		return Report{}
	}
	if !acquired {
		return Report{} // başka bir kopya bu turu üstlendi
	}

	return c.RunOnce(ctx, now)
}

// logRun — sadece bir şey silindiyse logla. Boş turlar log'u kirletmesin.
func (c *Cleaner) logRun(rep Report) {
	if rep.Total() == 0 {
		return
	}
	log.Printf("cleanup: deleted %d records (revoked=%d pending=%d refresh=%d)",
		rep.Total(), rep.RevokedTokens, rep.PendingActions, rep.RefreshTokens)
}
