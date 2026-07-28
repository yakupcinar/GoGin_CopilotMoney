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

// Tests for the hybrid auth flow.
//
//	access token  : returned in the JSON body (frontend keeps it in memory)
//	refresh token : HttpOnly cookie (JS cannot read it)
//
// These tests need no real DB or AI — they run against fake repositories.

// ---------------------------------------------------------------------------
// helpers
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

// loginFixture — sets up a logged-in user and the backing repositories.
func loginFixture(t *testing.T) (*gin.Engine, *fakeUserRepo, *fakeTokenRepo, *fakeRefreshRepo, *models.User) {
	t.Helper()
	os.Setenv("JWT_SECRET", "test-secret")
	t.Cleanup(func() { os.Unsetenv("JWT_SECRET") })

	hash, err := auth.HashPassword("dogrusifre123")
	if err != nil {
		t.Fatalf("hash could not be generated: %v", err)
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
		t.Fatalf("login failed: %d (%s)", w.Code, w.Body.String())
	}
	return w
}

func accessTokenOf(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var resp models.LoginResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response could not be parsed: %v", err)
	}
	return resp.Token
}

// ---------------------------------------------------------------------------
// Login
// ---------------------------------------------------------------------------

// Cookie attributes are what carry the security here: HttpOnly stops XSS,
// SameSite stops CSRF, and Path narrows the CSRF surface to a single
// endpoint. If any one of them regresses, the protection disappears
// silently — that is why we test them explicitly.
func TestLogin_SetsRefreshCookieWithSecureAttributes(t *testing.T) {
	r, _, _, _, _ := loginFixture(t)

	w := doLogin(t, r)

	ck := refreshCookieOf(w)
	if ck == nil {
		t.Fatal("no refresh cookie was set")
	}
	if !ck.HttpOnly {
		t.Error("not HttpOnly — JavaScript could read the token")
	}
	if ck.SameSite != http.SameSiteStrictMode {
		t.Errorf("expected SameSite=Strict, got %v", ck.SameSite)
	}
	if ck.Path != auth.RefreshCookiePath {
		t.Errorf("expected Path=%q, got %q", auth.RefreshCookiePath, ck.Path)
	}
	if ck.MaxAge <= 0 {
		t.Errorf("expected a positive MaxAge, got %d", ck.MaxAge)
	}
	if ck.Value == "" {
		t.Error("the cookie value is empty")
	}
}

// The access token must be returned in the body — the frontend keeps it in memory.
func TestLogin_ReturnsAccessTokenInBody(t *testing.T) {
	r, _, _, _, user := loginFixture(t)

	w := doLogin(t, r)

	claims, err := auth.ValidateToken(accessTokenOf(t, w))
	if err != nil {
		t.Fatalf("the returned access token failed validation: %v", err)
	}
	if claims.UserID != user.ID {
		t.Errorf("token carries the wrong user_id: %d", claims.UserID)
	}
}

// The raw refresh token must NEVER be stored — only its SHA-256 hash,
// so a database leak cannot hijack sessions.
func TestLogin_StoresHashNotRawToken(t *testing.T) {
	r, _, _, refreshRepo, _ := loginFixture(t)

	w := doLogin(t, r)
	raw := refreshCookieOf(w).Value

	if _, found := refreshRepo.tokens[raw]; found {
		t.Fatal("the RAW token was found in the store — it should have been hashed")
	}
	if _, found := refreshRepo.tokens[auth.HashRefreshToken(raw)]; !found {
		t.Fatal("the token's hash is missing from the store")
	}
}

// ---------------------------------------------------------------------------
// Refresh
// ---------------------------------------------------------------------------

// Every refresh must issue a NEW refresh token (rotation), so that a
// stolen token's useful life is bounded by the legitimate user's next refresh.
func TestRefresh_RotatesToken(t *testing.T) {
	r, _, _, _, _ := loginFixture(t)
	first := refreshCookieOf(doLogin(t, r))

	w := performWithCookie(r, "POST", "/auth/refresh", "", first)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	second := refreshCookieOf(w)
	if second == nil {
		t.Fatal("no new cookie was set on refresh")
	}
	if second.Value == first.Value {
		t.Fatal("the token did NOT rotate — rotation is not working")
	}
	if accessTokenOf(t, w) == "" {
		t.Fatal("no new access token was returned")
	}
}

func TestRefresh_WithoutCookie_Returns401(t *testing.T) {
	r, _, _, _, _ := loginFixture(t)

	w := performRequest(r, "POST", "/auth/refresh", "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestRefresh_UnknownToken_Returns401(t *testing.T) {
	r, _, _, _, _ := loginFixture(t)

	w := performWithCookie(r, "POST", "/auth/refresh", "",
		&http.Cookie{Name: auth.RefreshCookieName, Value: "hic-boyle-bir-token-yok"})

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

// LEAK DETECTION — the most important test in this file.
//
// If an already-consumed refresh token is presented again, either an
// attacker stole it or the legitimate user replayed an old one. We cannot
// tell which, so we err on the safe side and revoke ALL of the user's
// sessions — so that even if the attacker already grabbed the newest
// token, it becomes useless.
func TestRefresh_ReuseDetected_RevokesAllSessions(t *testing.T) {
	r, _, _, refreshRepo, _ := loginFixture(t)
	first := refreshCookieOf(doLogin(t, r))

	// Legitimate refresh: first is consumed, second is issued.
	okResp := performWithCookie(r, "POST", "/auth/refresh", "", first)
	second := refreshCookieOf(okResp)

	// An attacker (or the user) replays the OLD token.
	reuse := performWithCookie(r, "POST", "/auth/refresh", "", first)
	if reuse.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on reuse, got %d", reuse.Code)
	}

	// Critical: second, which was NEVER used, must also be invalid now.
	stored := refreshRepo.tokens[auth.HashRefreshToken(second.Value)]
	if stored == nil {
		t.Fatal("the second token was not found in the store")
	}
	if stored.RevokedAt == nil {
		t.Fatal("a leak was detected but the other sessions were NOT revoked")
	}

	// End-to-end check: refreshing with second must also fail.
	after := performWithCookie(r, "POST", "/auth/refresh", "", second)
	if after.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 after revocation, got %d", after.Code)
	}
}

// The role must NOT be embedded inside the refresh token: a user whose
// privileges were revoked must not keep the old ones for as long as the
// token remains valid. The user is read FRESH on every refresh.
func TestRefresh_UsesFreshRole(t *testing.T) {
	r, userRepo, _, _, user := loginFixture(t)
	first := refreshCookieOf(doLogin(t, r))

	// The user is made an admin in the meantime.
	userRepo.users[user.Username].Role = models.RoleAdmin

	w := performWithCookie(r, "POST", "/auth/refresh", "", first)
	if w.Code != http.StatusOK {
		t.Fatalf("beklenen 200, gelen %d", w.Code)
	}

	claims, err := auth.ValidateToken(accessTokenOf(t, w))
	if err != nil {
		t.Fatalf("token validation failed: %v", err)
	}
	if claims.Role != models.RoleAdmin {
		t.Fatalf("the new access token should carry the FRESH role, got %q", claims.Role)
	}
}

// If the user has been deleted, the session must not continue.
func TestRefresh_DeletedUser_Returns401(t *testing.T) {
	r, userRepo, _, _, user := loginFixture(t)
	first := refreshCookieOf(doLogin(t, r))

	delete(userRepo.users, user.Username)

	w := performWithCookie(r, "POST", "/auth/refresh", "", first)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Logout
// ---------------------------------------------------------------------------

// Logout must do three things at once:
//  1. clear the cookie from the browser
//  2. revoke the refresh token in the DB (clearing the cookie ALONE is not
//     enough: anyone who copied the value could keep using it)
//  3. blacklist the access token's jti (so it cannot work even for its
//     remaining 15 minutes)
func TestLogout_ClearsCookieAndRevokesBoth(t *testing.T) {
	r, _, tokenRepo, refreshRepo, _ := loginFixture(t)
	loginResp := doLogin(t, r)
	ck := refreshCookieOf(loginResp)

	w := performWithCookie(r, "POST", "/auth/logout", "", ck)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}

	// 1) was the cookie cleared
	cleared := refreshCookieOf(w)
	if cleared == nil {
		t.Fatal("logout did not send a cookie-clearing header")
	}
	if cleared.MaxAge >= 0 || cleared.Value != "" {
		t.Fatalf("the cookie was not cleared: value=%q maxAge=%d", cleared.Value, cleared.MaxAge)
	}

	// 2) was the refresh token revoked in the DB
	stored := refreshRepo.tokens[auth.HashRefreshToken(ck.Value)]
	if stored == nil || stored.RevokedAt == nil {
		t.Fatal("the refresh token was not revoked")
	}

	// 3) is the access token's jti on the denylist (authAs sets "test-jti")
	if !tokenRepo.revoked["test-jti"] {
		t.Fatal("the access token's jti was not revoked")
	}
}

// After logout, refreshing with the old refresh token must fail.
func TestLogout_ThenRefresh_Returns401(t *testing.T) {
	r, _, _, _, _ := loginFixture(t)
	ck := refreshCookieOf(doLogin(t, r))

	performWithCookie(r, "POST", "/auth/logout", "", ck)

	w := performWithCookie(r, "POST", "/auth/refresh", "", ck)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 after logout, got %d", w.Code)
	}
}
