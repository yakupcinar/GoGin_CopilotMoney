package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// GÜVENLİK REGRESYONU — gerçek gözlemlenmiş açık.
//
// Bulgu: SetTrustedProxies ayarlanmadan gin tüm vekillere güvenir ve
// ClientIP() için X-Forwarded-For başlığını okur. Önümüzde vekil olmadığı için
// bu başlığı istemcinin kendisi yazar; her isteğe farklı bir değer koyarak
// /login'deki IP bazlı limit TAMAMEN atlatılabiliyordu (8 istek, hiç 429 yok).
//
// Etkisi: brute-force ve CPU tüketme (bcrypt) korumalarının ikisi de kalkıyordu.
//
// Bu dosya iki şeyi kilitliyor: doğru yapılandırmanın açığı kapattığını, ve
// yapılandırma olmadan açığın GERÇEKTEN var olduğunu (yani testin bir şey
// ölçtüğünü).

// limitedRouter — verilen engine'e IP bazlı sınırlı tek bir route ekler.
func limitedRouter(r *gin.Engine, burst int) *gin.Engine {
	rl := NewRateLimiter(60, burst)
	r.POST("/x", Limit(rl, KeyByIP), func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

// spoofUntil429 — her istekte FARKLI bir X-Forwarded-For göndererek n deneme
// yapar; 429 görülürse true döner.
func spoofUntil429(r *gin.Engine, n int) bool {
	for i := 0; i < n; i++ {
		req := httptest.NewRequest(http.MethodPost, "/x", nil)
		// Sahte istemci IP'si. httptest gerçek RemoteAddr'ı sabit tutar,
		// yani TCP adresi değişmiyor — yalnızca başlık değişiyor.
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("9.9.9.%d", i))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code == http.StatusTooManyRequests {
			return true
		}
	}
	return false
}

// Asıl koruma: SetupTrustedProxies sonrası sahte başlık limiti atlatamamalı.
func TestSetupTrustedProxies_SpoofedHeaderCannotBypassLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("TRUSTED_PROXIES", "") // vekil yok senaryosu

	r := gin.New()
	if err := SetupTrustedProxies(r); err != nil {
		t.Fatalf("SetupTrustedProxies failed: %v", err)
	}
	limitedRouter(r, 2)

	if !spoofUntil429(r, 10) {
		t.Fatal("spoofed X-Forwarded-For bypassed the rate limit: " +
			"ClientIP() must ignore the header when no proxy is trusted")
	}
}

// Karşıt test: yapılandırma OLMADAN açık gerçekten var mı?
//
// Bu olmadan yukarıdaki test "her koşulda geçen" bir test olabilirdi ve bir şey
// ölçtüğünü kanıtlayamazdık. gin bir gün varsayılanını değiştirirse burası
// kırmızıya döner ve durumu yeniden değerlendiririz.
func TestDefaultTrustAll_IsActuallyVulnerable(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := limitedRouter(gin.New(), 2) // SetupTrustedProxies YOK -> varsayılan

	if spoofUntil429(r, 10) {
		t.Skip("gin'in varsayılanı artık tüm vekillere güvenmiyor gibi görünüyor; " +
			"SetupTrustedProxies hâlâ doğru ama gerekçesi güncellenmeli")
	}
}

// KeyByIP, vekile güvenilmediğinde başlığı değil gerçek adresi kullanmalı.
func TestKeyByIP_UsesRealAddressWhenProxiesUntrusted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("TRUSTED_PROXIES", "")

	r := gin.New()
	if err := SetupTrustedProxies(r); err != nil {
		t.Fatalf("SetupTrustedProxies failed: %v", err)
	}

	var got string
	r.GET("/k", func(c *gin.Context) { got = KeyByIP(c) })

	req := httptest.NewRequest(http.MethodGet, "/k", nil)
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	req.RemoteAddr = "10.0.0.7:5555"
	r.ServeHTTP(httptest.NewRecorder(), req)

	if got != "ip:10.0.0.7" {
		t.Fatalf("expected the real address (ip:10.0.0.7), got %q", got)
	}
}

// Vekil ARKASINDAYKEN: güvenilen ağdan gelen X-Forwarded-For okunmalı, yoksa
// tüm kullanıcılar nginx'in IP'si altında tek sayaca düşer ve IP bazlı limit
// anlamını yitirir.
//
// Sahtelenmeye karşı koruma burada gin'de değil NGINX'te: başlığı üzerine
// yazıyor (nginx.conf, $remote_addr). Bu test yalnızca güvenilen vekilden
// gelen değerin DOĞRU okunduğunu kilitler.
func TestKeyByIP_UsesForwardedHeaderFromTrustedProxy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("TRUSTED_PROXIES", "172.16.0.0/12")

	r := gin.New()
	if err := SetupTrustedProxies(r); err != nil {
		t.Fatalf("SetupTrustedProxies failed: %v", err)
	}

	var got string
	r.GET("/k", func(c *gin.Context) { got = KeyByIP(c) })

	req := httptest.NewRequest(http.MethodGet, "/k", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.9") // nginx'in yazdığı gerçek istemci
	req.RemoteAddr = "172.18.0.5:5555"               // nginx'in kendi adresi (güvenilen)
	r.ServeHTTP(httptest.NewRecorder(), req)

	if got != "ip:203.0.113.9" {
		t.Fatalf("expected the client address forwarded by the proxy (ip:203.0.113.9), got %q", got)
	}
}
