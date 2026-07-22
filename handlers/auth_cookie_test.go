package handlers

import (
	"GoGinMoneyCopilot/auth"
	"GoGinMoneyCopilot/models"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

// Hibrit auth akışının testleri.
//
//	access token  : JSON gövdesinde döner (frontend bellekte tutar)
//	refresh token : HttpOnly cookie (JS okuyamaz)
//
// Bu testler gerçek DB veya AI gerektirmez — sahte repo'larla çalışır.

// ---------------------------------------------------------------------------
// yardımcılar
// ---------------------------------------------------------------------------

func refreshCookieOf(w *httptest.ResponseRecorder) *http.Cookie {
	for _, ck := range w.Result().Cookies() {
		if ck.Name == auth.RefreshCookieName {
			return ck
		}
	}
	return nil
}

func performWithCookie(r *gin.Engine, method, path, body string, ck *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	if ck != nil {
		req.AddCookie(ck)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// loginFixture — giriş yapmış bir kullanıcı ve depoları hazırlar.
func loginFixture(t *testing.T) (*gin.Engine, *fakeUserRepo, *fakeTokenRepo, *fakeRefreshRepo, *models.User) {
	t.Helper()
	os.Setenv("JWT_SECRET", "test-secret")
	t.Cleanup(func() { os.Unsetenv("JWT_SECRET") })

	hash, err := auth.HashPassword("dogrusifre123")
	if err != nil {
		t.Fatalf("hash üretilemedi: %v", err)
	}
	userRepo := newFakeUserRepo()
	user := userRepo.seedUser("testuser", hash, models.RoleClient)
	tokenRepo := newFakeTokenRepo()
	refreshRepo := newFakeRefreshRepo()

	r := setupAuthRouterFull(userRepo, tokenRepo, refreshRepo)
	return r, userRepo, tokenRepo, refreshRepo, user
}

func doLogin(t *testing.T, r *gin.Engine) *httptest.ResponseRecorder {
	t.Helper()
	w := performRequest(r, "POST", "/login", `{"username":"testuser","password":"dogrusifre123"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("login başarısız: %d (%s)", w.Code, w.Body.String())
	}
	return w
}

func accessTokenOf(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var resp models.LoginResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("cevap parse edilemedi: %v", err)
	}
	return resp.Token
}

// ---------------------------------------------------------------------------
// Login
// ---------------------------------------------------------------------------

// Cookie öznitelikleri güvenliğin taşıyıcısı: HttpOnly XSS'i, SameSite CSRF'i,
// Path ise CSRF yüzeyini tek endpoint'e indirmeyi sağlıyor. Biri düşerse
// koruma sessizce kaybolur — o yüzden test ediyoruz.
func TestLogin_SetsRefreshCookieWithSecureAttributes(t *testing.T) {
	r, _, _, _, _ := loginFixture(t)

	w := doLogin(t, r)

	ck := refreshCookieOf(w)
	if ck == nil {
		t.Fatal("refresh cookie set edilmemiş")
	}
	if !ck.HttpOnly {
		t.Error("HttpOnly değil — JavaScript token'ı okuyabilir")
	}
	if ck.SameSite != http.SameSiteStrictMode {
		t.Errorf("SameSite=Strict bekleniyordu, gelen: %v", ck.SameSite)
	}
	if ck.Path != auth.RefreshCookiePath {
		t.Errorf("Path=%q bekleniyordu, gelen %q", auth.RefreshCookiePath, ck.Path)
	}
	if ck.MaxAge <= 0 {
		t.Errorf("MaxAge pozitif olmalı, gelen %d", ck.MaxAge)
	}
	if ck.Value == "" {
		t.Error("cookie değeri boş")
	}
}

// Access token gövdede dönmeli — frontend onu bellekte tutacak.
func TestLogin_ReturnsAccessTokenInBody(t *testing.T) {
	r, _, _, _, user := loginFixture(t)

	w := doLogin(t, r)

	claims, err := auth.ValidateToken(accessTokenOf(t, w))
	if err != nil {
		t.Fatalf("dönen access token doğrulanamadı: %v", err)
	}
	if claims.UserID != user.ID {
		t.Errorf("token yanlış user_id taşıyor: %d", claims.UserID)
	}
}

// Ham refresh token ASLA saklanmamalı — sadece SHA-256 hash'i.
// DB sızarsa oturumlar ele geçmesin diye.
func TestLogin_StoresHashNotRawToken(t *testing.T) {
	r, _, _, refreshRepo, _ := loginFixture(t)

	w := doLogin(t, r)
	raw := refreshCookieOf(w).Value

	if _, found := refreshRepo.tokens[raw]; found {
		t.Fatal("HAM token depoda bulundu — hash'lenmesi gerekiyordu")
	}
	if _, found := refreshRepo.tokens[auth.HashRefreshToken(raw)]; !found {
		t.Fatal("token'ın hash'i depoda yok")
	}
}

// ---------------------------------------------------------------------------
// Refresh
// ---------------------------------------------------------------------------

// Her yenilemede YENİ refresh token verilmeli (rotasyon).
// Çalınan bir token'ın ömrü, meşru kullanıcı bir sonraki yenilemeyi
// yapana kadar sınırlı kalsın diye.
func TestRefresh_RotatesToken(t *testing.T) {
	r, _, _, _, _ := loginFixture(t)
	first := refreshCookieOf(doLogin(t, r))

	w := performWithCookie(r, "POST", "/auth/refresh", "", first)

	if w.Code != http.StatusOK {
		t.Fatalf("beklenen 200, gelen %d (%s)", w.Code, w.Body.String())
	}
	second := refreshCookieOf(w)
	if second == nil {
		t.Fatal("yenilemede yeni cookie set edilmedi")
	}
	if second.Value == first.Value {
		t.Fatal("token DÖNMEDİ — rotasyon çalışmıyor")
	}
	if accessTokenOf(t, w) == "" {
		t.Fatal("yeni access token dönmedi")
	}
}

func TestRefresh_WithoutCookie_Returns401(t *testing.T) {
	r, _, _, _, _ := loginFixture(t)

	w := performRequest(r, "POST", "/auth/refresh", "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("beklenen 401, gelen %d", w.Code)
	}
}

func TestRefresh_UnknownToken_Returns401(t *testing.T) {
	r, _, _, _, _ := loginFixture(t)

	w := performWithCookie(r, "POST", "/auth/refresh", "",
		&http.Cookie{Name: auth.RefreshCookieName, Value: "hic-boyle-bir-token-yok"})

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("beklenen 401, gelen %d", w.Code)
	}
}

// SIZINTI TESPİTİ — bu dosyadaki en önemli test.
//
// Tüketilmiş bir refresh token tekrar sunulursa: ya saldırgan çaldı ya meşru
// kullanıcı eskisini oynatıyor. Ayırt edemeyiz, o yüzden güvenli tarafa
// geçip kullanıcının TÜM oturumlarını kapatıyoruz — saldırgan daha yeni
// token'ı ele geçirmiş olsa bile işe yaramasın.
func TestRefresh_ReuseDetected_RevokesAllSessions(t *testing.T) {
	r, _, _, refreshRepo, _ := loginFixture(t)
	first := refreshCookieOf(doLogin(t, r))

	// Meşru yenileme: first tüketilir, second üretilir.
	okResp := performWithCookie(r, "POST", "/auth/refresh", "", first)
	second := refreshCookieOf(okResp)

	// Saldırgan (ya da kullanıcı) ESKİ token'ı tekrar sunuyor.
	reuse := performWithCookie(r, "POST", "/auth/refresh", "", first)
	if reuse.Code != http.StatusUnauthorized {
		t.Fatalf("yeniden kullanımda 401 bekleniyordu, gelen %d", reuse.Code)
	}

	// Kritik: HENÜZ KULLANILMAMIŞ olan second de artık geçersiz olmalı.
	stored := refreshRepo.tokens[auth.HashRefreshToken(second.Value)]
	if stored == nil {
		t.Fatal("ikinci token depoda bulunamadı")
	}
	if stored.RevokedAt == nil {
		t.Fatal("sızıntı tespit edildi ama diğer oturumlar iptal EDİLMEDİ")
	}

	// Uçtan uca doğrulama: second ile yenileme de başarısız olmalı.
	after := performWithCookie(r, "POST", "/auth/refresh", "", second)
	if after.Code != http.StatusUnauthorized {
		t.Fatalf("iptal sonrası 401 bekleniyordu, gelen %d", after.Code)
	}
}

// Rol refresh token'ın İÇİNE gömülmemeli: yetkisi alınan kullanıcı,
// token'ı geçerli olduğu sürece eski yetkisini korumamalı.
// Her yenilemede kullanıcı TAZE okunuyor.
func TestRefresh_UsesFreshRole(t *testing.T) {
	r, userRepo, _, _, user := loginFixture(t)
	first := refreshCookieOf(doLogin(t, r))

	// Kullanıcı bu arada admin yapılıyor.
	userRepo.users[user.Username].Role = models.RoleAdmin

	w := performWithCookie(r, "POST", "/auth/refresh", "", first)
	if w.Code != http.StatusOK {
		t.Fatalf("beklenen 200, gelen %d", w.Code)
	}

	claims, err := auth.ValidateToken(accessTokenOf(t, w))
	if err != nil {
		t.Fatalf("token doğrulanamadı: %v", err)
	}
	if claims.Role != models.RoleAdmin {
		t.Fatalf("yeni access token TAZE rolü taşımalıydı, gelen %q", claims.Role)
	}
}

// Kullanıcı silinmişse oturum devam etmemeli.
func TestRefresh_DeletedUser_Returns401(t *testing.T) {
	r, userRepo, _, _, user := loginFixture(t)
	first := refreshCookieOf(doLogin(t, r))

	delete(userRepo.users, user.Username)

	w := performWithCookie(r, "POST", "/auth/refresh", "", first)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("beklenen 401, gelen %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Logout
// ---------------------------------------------------------------------------

// Logout üç işi birden yapmalı:
//  1. cookie'yi tarayıcıdan sil
//  2. refresh token'ı DB'de iptal et  (cookie silmek TEK BAŞINA yetmez:
//     değeri kopyalayan biri kullanmaya devam edebilirdi)
//  3. access token'ın jti'sini kara listeye al (15 dk da olsa çalışmasın)
func TestLogout_ClearsCookieAndRevokesBoth(t *testing.T) {
	r, _, tokenRepo, refreshRepo, _ := loginFixture(t)
	loginResp := doLogin(t, r)
	ck := refreshCookieOf(loginResp)

	w := performWithCookie(r, "POST", "/auth/logout", "", ck)
	if w.Code != http.StatusOK {
		t.Fatalf("beklenen 200, gelen %d (%s)", w.Code, w.Body.String())
	}

	// 1) cookie silinmiş mi
	cleared := refreshCookieOf(w)
	if cleared == nil {
		t.Fatal("logout cookie temizleme başlığı göndermedi")
	}
	if cleared.MaxAge >= 0 || cleared.Value != "" {
		t.Fatalf("cookie temizlenmemiş: value=%q maxAge=%d", cleared.Value, cleared.MaxAge)
	}

	// 2) refresh token DB'de iptal mi
	stored := refreshRepo.tokens[auth.HashRefreshToken(ck.Value)]
	if stored == nil || stored.RevokedAt == nil {
		t.Fatal("refresh token iptal edilmedi")
	}

	// 3) access token'ın jti'si kara listede mi (authAs "test-jti" set ediyor)
	if !tokenRepo.revoked["test-jti"] {
		t.Fatal("access token jti'si iptal edilmedi")
	}
}

// Logout'tan sonra eski refresh token ile yenileme yapılamamalı.
func TestLogout_ThenRefresh_Returns401(t *testing.T) {
	r, _, _, _, _ := loginFixture(t)
	ck := refreshCookieOf(doLogin(t, r))

	performWithCookie(r, "POST", "/auth/logout", "", ck)

	w := performWithCookie(r, "POST", "/auth/refresh", "", ck)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("logout sonrası 401 bekleniyordu, gelen %d", w.Code)
	}
}
