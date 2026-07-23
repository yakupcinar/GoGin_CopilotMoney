package models

import (
	"testing"
	"time"
)

// day — testlerde okunabilir tarih üretmek için.
func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// budgetOf — 5 Temmuz'da başlayan, kullanıcının seçtiği uzunlukta bütçe.
func budgetOf(start time.Time, periodDays int) Budget {
	return Budget{ID: 1, UserID: 1, Name: "Test", StartDate: start, PeriodDays: periodDays}
}

func assertPeriod(t *testing.T, p Period, wantIndex int, wantStart, wantEnd time.Time) {
	t.Helper()
	if p.Index != wantIndex {
		t.Fatalf("dönem indeksi: beklenen %d, gelen %d", wantIndex, p.Index)
	}
	if !p.Start.Equal(wantStart) {
		t.Fatalf("dönem başlangıcı: beklenen %s, gelen %s",
			wantStart.Format(DateLayout), p.Start.Format(DateLayout))
	}
	if !p.End.Equal(wantEnd) {
		t.Fatalf("dönem bitişi: beklenen %s, gelen %s",
			wantEnd.Format(DateLayout), p.End.Format(DateLayout))
	}
}

func TestPeriodAt_CurrentPeriodContainsToday(t *testing.T) {
	// Kullanıcının örneği: ayın 5'inde başla, 20 günde bir yenilen.
	b := budgetOf(day(2026, time.July, 5), 20)
	p := b.PeriodAt(day(2026, time.July, 13), 0)
	assertPeriod(t, p, 0, day(2026, time.July, 5), day(2026, time.July, 25))
}

func TestPeriodAt_StartDateIsFirstDayOfPeriodZero(t *testing.T) {
	b := budgetOf(day(2026, time.July, 5), 20)
	p := b.PeriodAt(day(2026, time.July, 5), 0)
	assertPeriod(t, p, 0, day(2026, time.July, 5), day(2026, time.July, 25))
}

func TestPeriodAt_ExactBoundaryStartsNextPeriod(t *testing.T) {
	// 25 Temmuz = start + 20 gün. Yarı açık aralık gereği bu gün ARTIK
	// 1. döneme aittir; 0. dönemde İKİNCİ kez sayılmaz.
	b := budgetOf(day(2026, time.July, 5), 20)
	p := b.PeriodAt(day(2026, time.July, 25), 0)
	assertPeriod(t, p, 1, day(2026, time.July, 25), day(2026, time.August, 14))
}

func TestPeriodAt_PreviousPeriodOffset(t *testing.T) {
	b := budgetOf(day(2026, time.July, 5), 20)
	p := b.PeriodAt(day(2026, time.July, 13), -1)
	assertPeriod(t, p, -1, day(2026, time.June, 15), day(2026, time.July, 5))
}

func TestPeriodAt_FutureStartDateGivesNegativeIndex(t *testing.T) {
	// floorDiv REGRESYON KORUMASI.
	// start gelecekte: fark negatif (-10 gün). Go'nun / operatörü sıfıra
	// doğru kırpsaydı (-10/30 == 0) dönem [1 Ağu, 31 Ağu) çıkardı ve bugünü
	// İÇERMEZDİ. Aşağı yuvarlama ile doğru cevap -1: [2 Tem, 1 Ağu).
	b := budgetOf(day(2026, time.August, 1), 30)
	today := day(2026, time.July, 22)
	p := b.PeriodAt(today, 0)

	assertPeriod(t, p, -1, day(2026, time.July, 2), day(2026, time.August, 1))
	if today.Before(p.Start) || !today.Before(p.End) {
		t.Fatalf("bugün (%s) dönemin dışında kaldı: [%s, %s)",
			today.Format(DateLayout), p.Start.Format(DateLayout), p.End.Format(DateLayout))
	}
}

func TestPeriodAt_PeriodsAreContiguous(t *testing.T) {
	// Dönemler arasında ne boşluk ne örtüşme olmalı: her dönemin bitişi bir
	// sonrakinin başlangıcıdır. Boşluk olsaydı o günlerin harcaması hiçbir
	// dönemde görünmezdi.
	b := budgetOf(day(2026, time.July, 5), 20)
	today := day(2026, time.July, 13)
	for n := -3; n < 3; n++ {
		cur := b.PeriodAt(today, n)
		next := b.PeriodAt(today, n+1)
		if !cur.End.Equal(next.Start) {
			t.Fatalf("dönem %d bitişi (%s) ile dönem %d başlangıcı (%s) uyuşmuyor",
				n, cur.End.Format(DateLayout), n+1, next.Start.Format(DateLayout))
		}
	}
}

func TestPeriodAt_SingleDayPeriod(t *testing.T) {
	b := budgetOf(day(2026, time.July, 5), 1)
	p := b.PeriodAt(day(2026, time.July, 13), 0)
	assertPeriod(t, p, 8, day(2026, time.July, 13), day(2026, time.July, 14))
}

func TestPeriodAt_IgnoresClockTime(t *testing.T) {
	// transaction_date ve start_date DATE kolonu — saat diye bir şey yok.
	// Günün 00:00'ı ile 23:59'u aynı döneme düşmeli.
	b := budgetOf(day(2026, time.July, 5), 20)
	early := time.Date(2026, time.July, 13, 0, 0, 0, 0, time.UTC)
	late := time.Date(2026, time.July, 13, 23, 59, 59, 0, time.UTC)

	if b.PeriodAt(early, 0).Index != b.PeriodAt(late, 0).Index {
		t.Fatalf("günün saati dönemi değiştirdi: %d vs %d",
			b.PeriodAt(early, 0).Index, b.PeriodAt(late, 0).Index)
	}
}

func TestPeriodAt_IgnoresTimeZone(t *testing.T) {
	// Aynı TAKVİM GÜNÜ farklı saat dilimlerinde aynı dönemi vermeli.
	// CivilDate t.Date()'i kendi diliminde okuduğu için bu sağlanır.
	b := budgetOf(day(2026, time.July, 5), 20)
	istanbul := time.FixedZone("+03", 3*60*60)

	inIstanbul := time.Date(2026, time.July, 13, 2, 0, 0, 0, istanbul)
	inUTC := time.Date(2026, time.July, 13, 2, 0, 0, 0, time.UTC)

	if b.PeriodAt(inIstanbul, 0).Index != b.PeriodAt(inUTC, 0).Index {
		t.Fatalf("saat dilimi dönemi değiştirdi: %d vs %d",
			b.PeriodAt(inIstanbul, 0).Index, b.PeriodAt(inUTC, 0).Index)
	}
}

func TestPeriodAt_LeapDayCrossing(t *testing.T) {
	// 2024 artık yıl: Şubat 29 çekiyor. AddDate takvim-doğru çalıştığı için
	// özel bir durum gerekmiyor; bu test onu sabitliyor.
	b := budgetOf(day(2024, time.February, 20), 20)
	p := b.PeriodAt(day(2024, time.March, 1), 0)
	assertPeriod(t, p, 0, day(2024, time.February, 20), day(2024, time.March, 11))
}

func TestPeriodAt_ZeroPeriodDaysDoesNotPanic(t *testing.T) {
	// Sıfır değerli struct sıfıra bölmeye yol açmamalı.
	b := Budget{ID: 1, UserID: 1, StartDate: day(2026, time.July, 5)}
	p := b.PeriodAt(day(2026, time.July, 13), 0)
	if p.End.Sub(p.Start) != 24*time.Hour {
		t.Fatalf("period_days=0 bir güne clamp'lenmeliydi, gelen %v", p.End.Sub(p.Start))
	}
}

func TestDaysRemaining_LastDayOfPeriod(t *testing.T) {
	b := budgetOf(day(2026, time.July, 5), 20)
	p := b.PeriodAt(day(2026, time.July, 24), 0)
	if got := p.DaysRemaining(day(2026, time.July, 24)); got != 1 {
		t.Fatalf("kalan gün: beklenen 1, gelen %d", got)
	}
}

func TestDaysRemaining_PastPeriodIsZero(t *testing.T) {
	b := budgetOf(day(2026, time.July, 5), 20)
	p := b.PeriodAt(day(2026, time.July, 13), -1)
	if got := p.DaysRemaining(day(2026, time.July, 13)); got != 0 {
		t.Fatalf("geçmiş dönemde kalan gün 0 olmalı, gelen %d", got)
	}
}

func TestDaysElapsed_ClampedToPeriodLength(t *testing.T) {
	b := budgetOf(day(2026, time.July, 5), 20)

	past := b.PeriodAt(day(2026, time.July, 13), -1)
	if got := past.DaysElapsed(day(2026, time.July, 13)); got != 20 {
		t.Fatalf("geçmiş dönemde geçen gün dönem uzunluğuna clamp'lenmeliydi, gelen %d", got)
	}

	future := b.PeriodAt(day(2026, time.July, 13), 1)
	if got := future.DaysElapsed(day(2026, time.July, 13)); got != 0 {
		t.Fatalf("gelecek dönemde geçen gün 0 olmalı, gelen %d", got)
	}
}

func TestCivilDate_StripsTimeOfDay(t *testing.T) {
	istanbul := time.FixedZone("+03", 3*60*60)
	got := CivilDate(time.Date(2026, time.July, 13, 14, 35, 12, 999, istanbul))
	want := day(2026, time.July, 13)
	if !got.Equal(want) {
		t.Fatalf("beklenen %s, gelen %s", want.Format(time.RFC3339), got.Format(time.RFC3339))
	}
}
