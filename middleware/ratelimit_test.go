package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestRateLimiter_BlocksAfterBurst(t *testing.T) {
	// Dakikada 60 (saniyede 1), patlama payı 3.
	rl := NewRateLimiter(60, 3)

	for i := 1; i <= 3; i++ {
		if !rl.Allow("ip:1.2.3.4") {
			t.Fatalf("%d. istek burst içindeyken reddedildi", i)
		}
	}
	if rl.Allow("ip:1.2.3.4") {
		t.Fatal("burst tükendikten sonra istek geçti")
	}
}

// Anahtarlar birbirini etkilememeli: bir kullanıcının limiti dolunca
// diğerleri engellenmemeli.
func TestRateLimiter_KeysAreIndependent(t *testing.T) {
	rl := NewRateLimiter(60, 1)

	if !rl.Allow("user:1") {
		t.Fatal("ilk kullanıcı engellendi")
	}
	if rl.Allow("user:1") {
		t.Fatal("aynı kullanıcı ikinci kez geçti")
	}
	if !rl.Allow("user:2") {
		t.Fatal("FARKLI kullanıcı, birincinin limiti yüzünden engellendi")
	}
}

// Sweep olmadan map her yeni IP ile büyür — bellek tüketme saldırısına açık.
func TestRateLimiter_SweepEvictsStaleKeys(t *testing.T) {
	rl := NewRateLimiter(60, 1)
	rl.ttl = 50 * time.Millisecond

	rl.Allow("ip:eski")
	if len(rl.visitors) != 1 {
		t.Fatalf("1 kayıt bekleniyordu, gelen %d", len(rl.visitors))
	}

	removed := rl.Sweep(time.Now().Add(time.Second))
	if removed != 1 || len(rl.visitors) != 0 {
		t.Fatalf("eski kayıt silinmedi (silinen=%d kalan=%d)", removed, len(rl.visitors))
	}
}

// Taze anahtar silinmemeli.
func TestRateLimiter_SweepKeepsFreshKeys(t *testing.T) {
	rl := NewRateLimiter(60, 1)

	rl.Allow("ip:taze")
	if removed := rl.Sweep(time.Now()); removed != 0 {
		t.Fatalf("taze anahtar silindi (%d)", removed)
	}
}

func TestLimit_Returns429WithRetryAfter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rl := NewRateLimiter(60, 1)

	r := gin.New()
	r.GET("/x", rl.Limit(KeyByIP), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	first := httptest.NewRecorder()
	r.ServeHTTP(first, httptest.NewRequest("GET", "/x", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("ilk istek %d döndü", first.Code)
	}

	second := httptest.NewRecorder()
	r.ServeHTTP(second, httptest.NewRequest("GET", "/x", nil))
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("beklenen 429, gelen %d", second.Code)
	}
	// Retry-After olmadan istemciler agresif yeniden dener, durumu kötüleştirir.
	if second.Header().Get("Retry-After") == "" {
		t.Fatal("Retry-After başlığı yok")
	}
}

// KeyByUser context'teki user_id'yi kullanmalı — aynı IP'deki farklı
// kullanıcılar birbirini engellemesin.
func TestKeyByUser_UsesUserIDNotIP(t *testing.T) {
	gin.SetMode(gin.TestMode)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Set("user_id", 42)

	if got := KeyByUser(c); got != "user:42" {
		t.Fatalf("beklenen user:42, gelen %q", got)
	}
}

// user_id yoksa (middleware sırası yanlışsa) IP'ye düşmeli — açık kalmamalı.
func TestKeyByUser_FallsBackToIP(t *testing.T) {
	gin.SetMode(gin.TestMode)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)

	if got := KeyByUser(c); got[:3] != "ip:" {
		t.Fatalf("IP'ye düşmesi bekleniyordu, gelen %q", got)
	}
}

// Geçersiz yapılandırma panic'e yol açmamalı (rate.NewLimiter 0 ile sorunlu).
func TestNewRateLimiter_RejectsNonPositive(t *testing.T) {
	rl := NewRateLimiter(0, 0)
	if !rl.Allow("k") {
		t.Fatal("varsayılana düşmeliydi, ilk istek reddedildi")
	}
}
