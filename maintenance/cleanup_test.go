package maintenance

import (
	"GoGinMoneyCopilot/models"
	"context"
	"errors"
	"testing"
	"time"
)

// The fake repositories for this package: only DeleteExpired is implemented
// meaningfully; the rest are empty because the tests never call them.

type fakeTokens struct {
	deleted int64
	err     error
	calls   int
}

func (f *fakeTokens) Revoke(context.Context, string, time.Time) error { return nil }
func (f *fakeTokens) IsRevoked(context.Context, string) (bool, error) { return false, nil }
func (f *fakeTokens) ListActive(context.Context, time.Time) ([]models.RevokedToken, error) {
	return nil, nil
}
func (f *fakeTokens) DeleteExpired(context.Context, time.Time) (int64, error) {
	f.calls++
	return f.deleted, f.err
}

type fakePending struct {
	deleted int64
	err     error
	calls   int
}

func (f *fakePending) Create(context.Context, *models.PendingAction) error { return nil }
func (f *fakePending) Claim(context.Context, int, string, time.Time) (*models.PendingAction, error) {
	return nil, nil
}
func (f *fakePending) DeleteExpired(context.Context, time.Time) (int64, error) {
	f.calls++
	return f.deleted, f.err
}

type fakeRefresh struct {
	deleted int64
	err     error
	calls   int
}

func (f *fakeRefresh) Create(context.Context, *models.RefreshToken) error { return nil }
func (f *fakeRefresh) Consume(context.Context, string, time.Time) (*models.RefreshToken, error) {
	return nil, nil
}
func (f *fakeRefresh) Revoke(context.Context, string, time.Time) error        { return nil }
func (f *fakeRefresh) RevokeAllForUser(context.Context, int, time.Time) error { return nil }
func (f *fakeRefresh) DeleteExpired(context.Context, time.Time) (int64, error) {
	f.calls++
	return f.deleted, f.err
}

func TestRunOnce_CleansAllThreeTables(t *testing.T) {
	tokens := &fakeTokens{deleted: 3}
	pending := &fakePending{deleted: 5}
	refresh := &fakeRefresh{deleted: 7}

	rep := NewCleaner(tokens, pending, refresh, time.Hour).RunOnce(context.Background(), time.Now())

	if rep.RevokedTokens != 3 || rep.PendingActions != 5 || rep.RefreshTokens != 7 {
		t.Fatalf("expected 3/5/7, got %d/%d/%d",
			rep.RevokedTokens, rep.PendingActions, rep.RefreshTokens)
	}
	if rep.Total() != 15 {
		t.Fatalf("expected a total of 15, got %d", rep.Total())
	}
}

// If one table fails the others must STILL be cleaned. A maintenance job is
// valuable even with partial success; stopping at the first error would
// waste two cleanups that could have run.
func TestRunOnce_ContinuesAfterError(t *testing.T) {
	tokens := &fakeTokens{err: errors.New("db blew up")}
	pending := &fakePending{deleted: 4}
	refresh := &fakeRefresh{deleted: 6}

	rep := NewCleaner(tokens, pending, refresh, time.Hour).RunOnce(context.Background(), time.Now())

	if pending.calls != 1 || refresh.calls != 1 {
		t.Fatalf("the other tables were not attempted after the error (pending=%d refresh=%d)",
			pending.calls, refresh.calls)
	}
	if rep.RevokedTokens != 0 {
		t.Fatalf("expected 0 for the failing table, got %d", rep.RevokedTokens)
	}
	if rep.Total() != 10 {
		t.Fatalf("expected 10 from the other two, got %d", rep.Total())
	}
}

// Start must run one pass immediately: records accumulated while the server
// was down should not wait for the first tick (1 hour by default).
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

	// Wait for the first pass to run.
	deadline := time.After(2 * time.Second)
	for tokens.calls == 0 {
		select {
		case <-deadline:
			t.Fatal("Start did not run a cleanup at startup")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	// On cancellation it must exit without waiting for the ticker.
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not stop on context cancellation")
	}
}

// If interval <= 0 is given it must fall back to a sensible default;
// otherwise time.NewTicker panics.
func TestNewCleaner_RejectsNonPositiveInterval(t *testing.T) {
	c := NewCleaner(&fakeTokens{}, &fakePending{}, &fakeRefresh{}, 0)
	if c.interval != DefaultInterval {
		t.Fatalf("expected the default interval, got %v", c.interval)
	}
}
