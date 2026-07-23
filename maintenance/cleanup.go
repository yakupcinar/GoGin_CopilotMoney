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
)

// DefaultInterval — saatte bir yeterli. Bu iş aceleye gelmez;
// amaç tabloyu sınırlı tutmak, anında temizlemek değil.
const DefaultInterval = time.Hour

type Cleaner struct {
	tokens   repositories.TokenRepository
	pending  repositories.PendingActionRepository
	refresh  repositories.RefreshTokenRepository
	interval time.Duration
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
func (c *Cleaner) RunOnce(now time.Time) Report {
	var rep Report

	if n, err := c.tokens.DeleteExpired(now); err != nil {
		log.Println("cleanup: revoked_tokens:", err)
	} else {
		rep.RevokedTokens = n
	}

	if n, err := c.pending.DeleteExpired(now); err != nil {
		log.Println("cleanup: pending_actions:", err)
	} else {
		rep.PendingActions = n
	}

	if n, err := c.refresh.DeleteExpired(now); err != nil {
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
	c.logRun(c.RunOnce(time.Now()))

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("cleanup worker stopped")
			return
		case <-ticker.C:
			c.logRun(c.RunOnce(time.Now()))
		}
	}
}

// logRun — sadece bir şey silindiyse logla. Boş turlar log'u kirletmesin.
func (c *Cleaner) logRun(rep Report) {
	if rep.Total() == 0 {
		return
	}
	log.Printf("cleanup: deleted %d records (revoked=%d pending=%d refresh=%d)",
		rep.Total(), rep.RevokedTokens, rep.PendingActions, rep.RefreshTokens)
}
