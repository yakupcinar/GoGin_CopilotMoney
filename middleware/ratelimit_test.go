package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestRateLimiter_BlocksAfterBurst(t *testing.T) {
	// 60 per minute (1 per second), burst allowance of 3.
	rl := NewRateLimiter(60, 3)

	for i := 1; i <= 3; i++ {
		if !rl.Allow(context.Background(), "ip:1.2.3.4") {
			t.Fatalf("request %d was rejected while still within the burst", i)
		}
	}
	if rl.Allow(context.Background(), "ip:1.2.3.4") {
		t.Fatal("a request passed after the burst was exhausted")
	}
}

// Keys must not affect each other: one user hitting their limit must not
// block the others.
func TestRateLimiter_KeysAreIndependent(t *testing.T) {
	rl := NewRateLimiter(60, 1)

	if !rl.Allow(context.Background(), "user:1") {
		t.Fatal("the first user was blocked")
	}
	if rl.Allow(context.Background(), "user:1") {
		t.Fatal("the same user passed a second time")
	}
	if !rl.Allow(context.Background(), "user:2") {
		t.Fatal("a DIFFERENT user was blocked because of the first user's limit")
	}
}

// Without Sweep the map grows with every new IP — open to a memory
// exhaustion attack.
func TestRateLimiter_SweepEvictsStaleKeys(t *testing.T) {
	rl := NewRateLimiter(60, 1)
	rl.ttl = 50 * time.Millisecond

	rl.Allow(context.Background(), "ip:eski")
	if len(rl.visitors) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(rl.visitors))
	}

	removed := rl.Sweep(time.Now().Add(time.Second))
	if removed != 1 || len(rl.visitors) != 0 {
		t.Fatalf("the stale entry was not evicted (removed=%d remaining=%d)", removed, len(rl.visitors))
	}
}

// A fresh key must not be evicted.
func TestRateLimiter_SweepKeepsFreshKeys(t *testing.T) {
	rl := NewRateLimiter(60, 1)

	rl.Allow(context.Background(), "ip:taze")
	if removed := rl.Sweep(time.Now()); removed != 0 {
		t.Fatalf("a fresh key was evicted (%d)", removed)
	}
}

func TestLimit_Returns429WithRetryAfter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rl := NewRateLimiter(60, 1)

	r := gin.New()
	r.GET("/x", Limit(rl, KeyByIP), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	first := httptest.NewRecorder()
	r.ServeHTTP(first, httptest.NewRequest("GET", "/x", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("the first request returned %d", first.Code)
	}

	second := httptest.NewRecorder()
	r.ServeHTTP(second, httptest.NewRequest("GET", "/x", nil))
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", second.Code)
	}
	// Without Retry-After clients retry aggressively and make things worse.
	if second.Header().Get("Retry-After") == "" {
		t.Fatal("the Retry-After header is missing")
	}
}

// KeyByUser must use the user_id from the context — different users behind
// the same IP must not block each other.
func TestKeyByUser_UsesUserIDNotIP(t *testing.T) {
	gin.SetMode(gin.TestMode)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Set("user_id", 42)

	if got := KeyByUser(c); got != "user:42" {
		t.Fatalf("expected user:42, got %q", got)
	}
}

// If user_id is absent (wrong middleware order) it must fall back to the IP
// rather than leaving the endpoint unprotected.
func TestKeyByUser_FallsBackToIP(t *testing.T) {
	gin.SetMode(gin.TestMode)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)

	if got := KeyByUser(c); got[:3] != "ip:" {
		t.Fatalf("expected a fallback to the IP, got %q", got)
	}
}

// Invalid configuration must not cause a panic (rate.NewLimiter misbehaves
// with 0).
func TestNewRateLimiter_RejectsNonPositive(t *testing.T) {
	rl := NewRateLimiter(0, 0)
	if !rl.Allow(context.Background(), "k") {
		t.Fatal("expected a fallback to the default; the first request was rejected")
	}
}
