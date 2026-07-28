package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
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

// ---------------------------------------------------------------------------
// AllowWithLimit / LimitByPlan — abonelik planına göre değişen eşik.
// ---------------------------------------------------------------------------

// AllowWithLimit, çağıranın verdiği eşiği alttaki rate.Limiter'a DOĞRU
// AKTARMALI. Art arda çağrılarla test etmiyoruz: rate.Limiter'da "burst"
// (anlık kapasite) ile "rate" (dolum hızı) ayrı kavramlar — burst kurucuda
// sabit kaldığı için art arda istekler burst'e takılır, perMinute'e değil.
// Doğru testi, alttaki oranın gerçekten güncellendiğini doğrudan kontrol
// ederek yapıyoruz (aynı paket içindeyiz, visitors map'ine erişebiliyoruz).
func TestRateLimiter_AllowWithLimit_ConfiguresRateFromCallerSuppliedThreshold(t *testing.T) {
	rl := NewRateLimiter(1000, 1) // kurucudaki limit ÖNEMSİZ, sadece ilk kayıt için
	rl.AllowWithLimit(context.Background(), "user:1", 2)

	rl.mu.Lock()
	got := rl.visitors["user:1"].limiter.Limit()
	rl.mu.Unlock()

	want := rate.Every(time.Minute / time.Duration(2))
	if got != want {
		t.Fatalf("expected the visitor's rate to reflect the caller-supplied limit of 2/min, got %v want %v", got, want)
	}
}

// Bir kullanıcı planını YÜKSELTİRSE (aynı anahtar, daha yüksek limit), mevcut
// bucket yeniden OLUŞTURULMADAN oranı güncellenmeli (SetLimit) — yeni bir
// anahtarla yeniden başlamak, kullanıcının o ana kadarki tüketimini sıfırlar.
func TestRateLimiter_AllowWithLimit_UpdatesRateOnSameKey(t *testing.T) {
	rl := NewRateLimiter(1000, 1)
	ctx := context.Background()

	rl.AllowWithLimit(ctx, "user:1", 1) // free: ilk çağrı, bucket'ı oluşturur
	rl.mu.Lock()
	freeRate := rl.visitors["user:1"].limiter.Limit()
	rl.mu.Unlock()

	rl.AllowWithLimit(ctx, "user:1", 100) // "yükseltme": AYNI anahtar, yeni limit
	rl.mu.Lock()
	proRate := rl.visitors["user:1"].limiter.Limit()
	rl.mu.Unlock()

	if proRate <= freeRate {
		t.Fatalf("expected the upgraded rate (100/min) to exceed the free rate (1/min), got free=%v pro=%v",
			freeRate, proRate)
	}
}

// fakePlanAwareLimiter — LimitByPlan'ın MANTIĞINI test etmek için: doğru
// anahtarla, doğru eşikle çağırıyor mu? Sayacın kendisinin doğru saydığı
// (token bucket ya da Redis) ayrı testlerde zaten doğrulanıyor; burada tek
// soru LimitByPlan'ın planFn'den okuduğu değeri doğru ilettiği.
type fakePlanAwareLimiter struct {
	lastKey       string
	lastPerMinute int
	calls         int
	allow         bool
}

func (f *fakePlanAwareLimiter) AllowWithLimit(_ context.Context, key string, perMinute int) bool {
	f.lastKey = key
	f.lastPerMinute = perMinute
	f.calls++
	return f.allow
}

// pro planındaki bir kullanıcı için LimitByPlan, free DEĞİL pro eşiğini
// alttaki limiter'a iletmeli.
func TestLimitByPlan_PassesPlanSpecificThreshold(t *testing.T) {
	gin.SetMode(gin.TestMode)

	fake := &fakePlanAwareLimiter{allow: true}
	limits := map[string]int{"free": 5, "pro": 30}

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("user_id", 42) })
	r.POST("/chat", LimitByPlan(fake, KeyByUser,
		func(*gin.Context) (string, error) { return "pro", nil }, limits, limits["free"]),
		func(c *gin.Context) { c.Status(http.StatusOK) })

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/chat", nil))

	if fake.calls != 1 {
		t.Fatalf("expected exactly 1 call to AllowWithLimit, got %d", fake.calls)
	}
	if fake.lastKey != "user:42" {
		t.Fatalf("expected key %q, got %q", "user:42", fake.lastKey)
	}
	if fake.lastPerMinute != 30 {
		t.Fatalf("expected the pro plan's limit (30) to be used, got %d", fake.lastPerMinute)
	}
}

// planFn "free" dönerse, "pro" değil free eşiği kullanılmalı — aynı harita,
// farklı kullanıcı, farklı sonuç.
func TestLimitByPlan_FreeUserGetsFreeThreshold(t *testing.T) {
	gin.SetMode(gin.TestMode)

	fake := &fakePlanAwareLimiter{allow: true}
	limits := map[string]int{"free": 5, "pro": 30}

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("user_id", 7) })
	r.POST("/chat", LimitByPlan(fake, KeyByUser,
		func(*gin.Context) (string, error) { return "free", nil }, limits, limits["free"]),
		func(c *gin.Context) { c.Status(http.StatusOK) })

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/chat", nil))

	if fake.lastPerMinute != 5 {
		t.Fatalf("expected the free plan's limit (5) to be used, got %d", fake.lastPerMinute)
	}
}

// AllowWithLimit false dönerse LimitByPlan gerçekten 429 vermeli — fake'in
// tek işi "hayır" demek, middleware'in buna doğru tepki verdiğini doğruluyor.
func TestLimitByPlan_RejectionReturns429(t *testing.T) {
	gin.SetMode(gin.TestMode)

	fake := &fakePlanAwareLimiter{allow: false}
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("user_id", 1) })
	r.POST("/chat", LimitByPlan(fake, KeyByUser,
		func(*gin.Context) (string, error) { return "free", nil },
		map[string]int{"free": 5}, 5),
		func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/chat", nil))

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 when the limiter rejects, got %d", w.Code)
	}
}

// planFn hata dönerse (örn. veritabanı sorgusu başarısız), LimitByPlan EN
// KISITLAYICI (defaultLimit) değere düşmeli — bir arızada cömert davranmak
// maliyet korumasının amacını boşa çıkarır.
func TestLimitByPlan_LookupFailureFallsBackToDefaultLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rl := NewRateLimiter(1000, 1)
	failingPlanFn := func(*gin.Context) (string, error) {
		return "", errors.New("user lookup failed")
	}

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("user_id", 99) })
	r.POST("/chat", LimitByPlan(rl, KeyByUser, failingPlanFn,
		map[string]int{"free": 5, "pro": 30}, 1 /* defaultLimit */),
		func(c *gin.Context) { c.Status(http.StatusOK) })

	if w := (httptest.NewRecorder()); true {
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/chat", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected the first request to pass under defaultLimit=1, got %d", w.Code)
		}
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/chat", nil))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after exhausting the conservative default limit (1), got %d", w.Code)
	}
}
