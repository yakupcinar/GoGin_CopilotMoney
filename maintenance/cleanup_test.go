package maintenance

import (
	"GoGinMoneyCopilot/models"
	"context"
	"errors"
	"testing"
	"time"
)

// Bu paketin sahte depoları: yalnızca DeleteExpired'ı anlamlı biçimde
// uyguluyor, geri kalanı testte kullanılmadığı için boş.

type fakeTokens struct {
	deleted int64
	err     error
	calls   int
}

func (f *fakeTokens) Revoke(string, time.Time) error { return nil }
func (f *fakeTokens) IsRevoked(string) (bool, error) { return false, nil }
func (f *fakeTokens) DeleteExpired(time.Time) (int64, error) {
	f.calls++
	return f.deleted, f.err
}

type fakePending struct {
	deleted int64
	err     error
	calls   int
}

func (f *fakePending) Create(*models.PendingAction) error { return nil }
func (f *fakePending) Claim(int, string, time.Time) (*models.PendingAction, error) {
	return nil, nil
}
func (f *fakePending) DeleteExpired(time.Time) (int64, error) {
	f.calls++
	return f.deleted, f.err
}

type fakeRefresh struct {
	deleted int64
	err     error
	calls   int
}

func (f *fakeRefresh) Create(*models.RefreshToken) error { return nil }
func (f *fakeRefresh) Consume(string, time.Time) (*models.RefreshToken, error) {
	return nil, nil
}
func (f *fakeRefresh) Revoke(string, time.Time) error        { return nil }
func (f *fakeRefresh) RevokeAllForUser(int, time.Time) error { return nil }
func (f *fakeRefresh) DeleteExpired(time.Time) (int64, error) {
	f.calls++
	return f.deleted, f.err
}

func TestRunOnce_CleansAllThreeTables(t *testing.T) {
	tokens := &fakeTokens{deleted: 3}
	pending := &fakePending{deleted: 5}
	refresh := &fakeRefresh{deleted: 7}

	rep := NewCleaner(tokens, pending, refresh, time.Hour).RunOnce(time.Now())

	if rep.RevokedTokens != 3 || rep.PendingActions != 5 || rep.RefreshTokens != 7 {
		t.Fatalf("beklenen 3/5/7, gelen %d/%d/%d",
			rep.RevokedTokens, rep.PendingActions, rep.RefreshTokens)
	}
	if rep.Total() != 15 {
		t.Fatalf("toplam 15 bekleniyordu, gelen %d", rep.Total())
	}
}

// Bir tablo hata verirse diğerleri temizlenmeye DEVAM etmeli.
// Bakım işi kısmi başarıyla da değerlidir; ilk hatada durmak,
// çalışabilecek iki temizliği de boşa çıkarır.
func TestRunOnce_ContinuesAfterError(t *testing.T) {
	tokens := &fakeTokens{err: errors.New("db patladı")}
	pending := &fakePending{deleted: 4}
	refresh := &fakeRefresh{deleted: 6}

	rep := NewCleaner(tokens, pending, refresh, time.Hour).RunOnce(time.Now())

	if pending.calls != 1 || refresh.calls != 1 {
		t.Fatalf("hata sonrası diğer tablolar denenmedi (pending=%d refresh=%d)",
			pending.calls, refresh.calls)
	}
	if rep.RevokedTokens != 0 {
		t.Fatalf("hatalı tablo için 0 bekleniyordu, gelen %d", rep.RevokedTokens)
	}
	if rep.Total() != 10 {
		t.Fatalf("diğer ikisinden 10 bekleniyordu, gelen %d", rep.Total())
	}
}

// Start hemen bir tur çalıştırmalı: sunucu kapalıyken biriken kayıtlar
// ilk tick'i (varsayılan 1 saat) beklememeli.
func TestStart_RunsImmediatelyAndStopsOnCancel(t *testing.T) {
	tokens := &fakeTokens{}
	pending := &fakePending{}
	refresh := &fakeRefresh{}
	cleaner := NewCleaner(tokens, pending, refresh, time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		cleaner.Start(ctx)
		close(done)
	}()

	// İlk turun çalışmasını bekle.
	deadline := time.After(2 * time.Second)
	for tokens.calls == 0 {
		select {
		case <-deadline:
			t.Fatal("Start başlangıçta temizlik yapmadı")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	// İptal edilince ticker'ı beklemeden çıkmalı.
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Start context iptalinde durmadı")
	}
}

// interval <= 0 verilirse makul bir varsayılana düşmeli;
// aksi halde time.NewTicker panic atar.
func TestNewCleaner_RejectsNonPositiveInterval(t *testing.T) {
	c := NewCleaner(&fakeTokens{}, &fakePending{}, &fakeRefresh{}, 0)
	if c.interval != DefaultInterval {
		t.Fatalf("varsayılan aralık bekleniyordu, gelen %v", c.interval)
	}
}
