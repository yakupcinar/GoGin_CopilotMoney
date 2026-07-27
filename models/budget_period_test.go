package models

import (
	"testing"
	"time"
)

// day — produces readable dates for the tests.
func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// budgetOf — a budget starting on the given date with a caller-chosen length.
func budgetOf(start time.Time, periodDays int) Budget {
	return Budget{ID: 1, UserID: 1, Name: "Test", StartDate: start, PeriodDays: periodDays}
}

func assertPeriod(t *testing.T, p Period, wantIndex int, wantStart, wantEnd time.Time) {
	t.Helper()
	if p.Index != wantIndex {
		t.Fatalf("period index: expected %d, got %d", wantIndex, p.Index)
	}
	if !p.Start.Equal(wantStart) {
		t.Fatalf("period start: expected %s, got %s",
			wantStart.Format(DateLayout), p.Start.Format(DateLayout))
	}
	if !p.End.Equal(wantEnd) {
		t.Fatalf("period end: expected %s, got %s",
			wantEnd.Format(DateLayout), p.End.Format(DateLayout))
	}
}

func TestPeriodAt_CurrentPeriodContainsToday(t *testing.T) {
	// The user's example: start on the 5th, renew every 20 days.
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
	// July 25 = start + 20 days. Because the interval is half-open this day
	// ALREADY belongs to period 1; it is not counted a SECOND time in period 0.
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
	// floorDiv REGRESSION GUARD.
	// The start is in the future: the difference is negative (-10 days). If
	// Go's / operator truncated toward zero (-10/30 == 0) the period would be
	// [Aug 1, Aug 31) and would NOT contain today. Flooring gives the correct
	// answer of -1: [Jul 2, Aug 1).
	b := budgetOf(day(2026, time.August, 1), 30)
	today := day(2026, time.July, 22)
	p := b.PeriodAt(today, 0)

	assertPeriod(t, p, -1, day(2026, time.July, 2), day(2026, time.August, 1))
	if today.Before(p.Start) || !today.Before(p.End) {
		t.Fatalf("today (%s) fell outside the period: [%s, %s)",
			today.Format(DateLayout), p.Start.Format(DateLayout), p.End.Format(DateLayout))
	}
}

func TestPeriodAt_PeriodsAreContiguous(t *testing.T) {
	// There must be neither a gap nor an overlap between periods: each period
	// ends exactly where the next begins. A gap would make the spending on
	// those days invisible in every period.
	b := budgetOf(day(2026, time.July, 5), 20)
	today := day(2026, time.July, 13)
	for n := -3; n < 3; n++ {
		cur := b.PeriodAt(today, n)
		next := b.PeriodAt(today, n+1)
		if !cur.End.Equal(next.Start) {
			t.Fatalf("end of period %d (%s) does not meet the start of period %d (%s)",
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
	// transaction_date and start_date are DATE columns — there is no clock
	// time. 00:00 and 23:59 of the same day must fall in the same period.
	b := budgetOf(day(2026, time.July, 5), 20)
	early := time.Date(2026, time.July, 13, 0, 0, 0, 0, time.UTC)
	late := time.Date(2026, time.July, 13, 23, 59, 59, 0, time.UTC)

	if b.PeriodAt(early, 0).Index != b.PeriodAt(late, 0).Index {
		t.Fatalf("the time of day changed the period: %d vs %d",
			b.PeriodAt(early, 0).Index, b.PeriodAt(late, 0).Index)
	}
}

func TestPeriodAt_IgnoresTimeZone(t *testing.T) {
	// The same CALENDAR DAY must yield the same period across time zones.
	// This holds because CivilDate reads t.Date() in the value's own zone.
	b := budgetOf(day(2026, time.July, 5), 20)
	istanbul := time.FixedZone("+03", 3*60*60)

	inIstanbul := time.Date(2026, time.July, 13, 2, 0, 0, 0, istanbul)
	inUTC := time.Date(2026, time.July, 13, 2, 0, 0, 0, time.UTC)

	if b.PeriodAt(inIstanbul, 0).Index != b.PeriodAt(inUTC, 0).Index {
		t.Fatalf("the time zone changed the period: %d vs %d",
			b.PeriodAt(inIstanbul, 0).Index, b.PeriodAt(inUTC, 0).Index)
	}
}

func TestPeriodAt_LeapDayCrossing(t *testing.T) {
	// 2024 is a leap year: February has 29 days. No special case is needed
	// because AddDate is calendar-correct; this test pins that down.
	b := budgetOf(day(2024, time.February, 20), 20)
	p := b.PeriodAt(day(2024, time.March, 1), 0)
	assertPeriod(t, p, 0, day(2024, time.February, 20), day(2024, time.March, 11))
}

func TestPeriodAt_ZeroPeriodDaysDoesNotPanic(t *testing.T) {
	// A zero-valued struct must not lead to a division by zero.
	b := Budget{ID: 1, UserID: 1, StartDate: day(2026, time.July, 5)}
	p := b.PeriodAt(day(2026, time.July, 13), 0)
	if p.End.Sub(p.Start) != 24*time.Hour {
		t.Fatalf("period_days=0 should have been clamped to one day, got %v", p.End.Sub(p.Start))
	}
}

func TestDaysRemaining_LastDayOfPeriod(t *testing.T) {
	b := budgetOf(day(2026, time.July, 5), 20)
	p := b.PeriodAt(day(2026, time.July, 24), 0)
	if got := p.DaysRemaining(day(2026, time.July, 24)); got != 1 {
		t.Fatalf("days remaining: expected 1, got %d", got)
	}
}

func TestDaysRemaining_PastPeriodIsZero(t *testing.T) {
	b := budgetOf(day(2026, time.July, 5), 20)
	p := b.PeriodAt(day(2026, time.July, 13), -1)
	if got := p.DaysRemaining(day(2026, time.July, 13)); got != 0 {
		t.Fatalf("days remaining must be 0 for a past period, got %d", got)
	}
}

func TestDaysElapsed_ClampedToPeriodLength(t *testing.T) {
	b := budgetOf(day(2026, time.July, 5), 20)

	past := b.PeriodAt(day(2026, time.July, 13), -1)
	if got := past.DaysElapsed(day(2026, time.July, 13)); got != 20 {
		t.Fatalf("days elapsed must be clamped to the period length for a past period, got %d", got)
	}

	future := b.PeriodAt(day(2026, time.July, 13), 1)
	if got := future.DaysElapsed(day(2026, time.July, 13)); got != 0 {
		t.Fatalf("days elapsed must be 0 for a future period, got %d", got)
	}
}

func TestCivilDate_StripsTimeOfDay(t *testing.T) {
	istanbul := time.FixedZone("+03", 3*60*60)
	got := CivilDate(time.Date(2026, time.July, 13, 14, 35, 12, 999, istanbul))
	want := day(2026, time.July, 13)
	if !got.Equal(want) {
		t.Fatalf("expected %s, got %s", want.Format(time.RFC3339), got.Format(time.RFC3339))
	}
}
