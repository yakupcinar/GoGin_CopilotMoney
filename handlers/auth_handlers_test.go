package handlers

import (
	"GoGinMoneyCopilot/auth"
	"GoGinMoneyCopilot/models"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func setupAuthRouter(userRepo *fakeUserRepo, tokenRepo *fakeTokenRepo) *gin.Engine {
	return setupAuthRouterFull(userRepo, tokenRepo, newFakeRefreshRepo())
}

// setupAuthRouterFull — for tests that also need access to the refresh repo.
func setupAuthRouterFull(userRepo *fakeUserRepo, tokenRepo *fakeTokenRepo, refreshRepo *fakeRefreshRepo) *gin.Engine {
	h := NewAuthHandler(userRepo, tokenRepo, refreshRepo)
	r := gin.New()
	r.POST("/register", h.Register)
	r.POST("/login", h.Login)
	// /auth/refresh is unprotected: we are here precisely because the access
	// token has expired.
	r.POST("/auth/refresh", h.Refresh)
	// logout is protected: authAs mimics the values AuthMiddleware would set
	r.POST("/auth/logout", authAs(1, models.RoleClient), h.Logout)
	return r
}

func TestRegister_Success(t *testing.T) {
	userRepo := newFakeUserRepo()
	r := setupAuthRouter(userRepo, newFakeTokenRepo())

	w := performRequest(r, "POST", "/register", `{"username":"yenikullanici","password":"gizlisifre123"}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (body: %s)", w.Code, w.Body.String())
	}
	user, ok := userRepo.users["yenikullanici"]
	if !ok {
		t.Fatalf("user was not added to the repo")
	}
	// The password must be stored hashed, NOT in plain text
	if user.PasswordHash == "gizlisifre123" {
		t.Fatalf("password was stored in plain text!")
	}
	if !auth.CheckPassword("gizlisifre123", user.PasswordHash) {
		t.Fatalf("the stored hash does not match the password")
	}
	// A new user must default to the client role
	if user.Role != models.RoleClient {
		t.Fatalf("expected the default role to be client, got %q", user.Role)
	}
}

func TestRegister_DuplicateUsername(t *testing.T) {
	userRepo := newFakeUserRepo()
	userRepo.seedUser("mevcut", "hash", models.RoleClient)
	r := setupAuthRouter(userRepo, newFakeTokenRepo())

	w := performRequest(r, "POST", "/register", `{"username":"mevcut","password":"gizlisifre123"}`)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
}

func TestRegister_ShortPasswordRejected(t *testing.T) {
	userRepo := newFakeUserRepo()
	r := setupAuthRouter(userRepo, newFakeTokenRepo())

	// binding min=8 -> 400
	w := performRequest(r, "POST", "/register", `{"username":"kullanici","password":"kisa"}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if len(userRepo.users) != 0 {
		t.Fatalf("a user was created for invalid input")
	}
}

func TestLogin_Success(t *testing.T) {
	os.Setenv("JWT_SECRET", "test-secret")
	defer os.Unsetenv("JWT_SECRET")

	hash, err := auth.HashPassword("dogrusifre123")
	if err != nil {
		t.Fatalf("hash could not be generated: %v", err)
	}
	userRepo := newFakeUserRepo()
	userRepo.seedUser("testuser", hash, models.RoleClient)
	r := setupAuthRouter(userRepo, newFakeTokenRepo())

	w := performRequest(r, "POST", "/login", `{"username":"testuser","password":"dogrusifre123"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}
	var resp models.LoginResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response could not be parsed: %v", err)
	}
	if resp.Token == "" {
		t.Fatalf("an empty token was returned")
	}
	// The returned token must actually validate and carry the correct user
	claims, err := auth.ValidateToken(resp.Token)
	if err != nil {
		t.Fatalf("the returned token failed validation: %v", err)
	}
	if claims.UserID != userRepo.users["testuser"].ID {
		t.Fatalf("token carries the wrong user_id: %d", claims.UserID)
	}
	if claims.Role != models.RoleClient {
		t.Fatalf("token carries the wrong role: %q", claims.Role)
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	hash, _ := auth.HashPassword("dogrusifre123")
	userRepo := newFakeUserRepo()
	userRepo.seedUser("testuser", hash, models.RoleClient)
	r := setupAuthRouter(userRepo, newFakeTokenRepo())

	w := performRequest(r, "POST", "/login", `{"username":"testuser","password":"yanlissifre"}`)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestLogin_UnknownUser(t *testing.T) {
	r := setupAuthRouter(newFakeUserRepo(), newFakeTokenRepo())

	w := performRequest(r, "POST", "/login", `{"username":"olmayan","password":"herhangibirsifre"}`)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

// Whether a username exists must not leak through the error message:
// both cases must return the same message.
func TestLogin_SameErrorMessageForBothFailures(t *testing.T) {
	hash, _ := auth.HashPassword("dogrusifre123")
	userRepo := newFakeUserRepo()
	userRepo.seedUser("testuser", hash, models.RoleClient)
	r := setupAuthRouter(userRepo, newFakeTokenRepo())

	wrongPass := performRequest(r, "POST", "/login", `{"username":"testuser","password":"yanlis"}`)
	noUser := performRequest(r, "POST", "/login", `{"username":"olmayan","password":"yanlis"}`)

	if wrongPass.Body.String() != noUser.Body.String() {
		t.Fatalf("error messages differ, username existence leaks:\n  wrong password: %s\n  unknown user: %s",
			wrongPass.Body.String(), noUser.Body.String())
	}
}

// Login timing: because bcrypt is also run when the user does not exist
// (dummyHash), the two failure paths must not differ significantly in
// duration (timing side-channel protection).
func TestLogin_TimingSimilarForBothFailures(t *testing.T) {
	hash, _ := auth.HashPassword("dogrusifre123")
	userRepo := newFakeUserRepo()
	userRepo.seedUser("testuser", hash, models.RoleClient)
	r := setupAuthRouter(userRepo, newFakeTokenRepo())

	start := time.Now()
	performRequest(r, "POST", "/login", `{"username":"testuser","password":"yanlis"}`)
	wrongPassDuration := time.Since(start)

	start = time.Now()
	performRequest(r, "POST", "/login", `{"username":"olmayan","password":"yanlis"}`)
	noUserDuration := time.Since(start)

	ratio := float64(wrongPassDuration) / float64(noUserDuration)
	if ratio < 0.25 || ratio > 4 {
		t.Fatalf("suspicious timing gap between the two failure paths (ratio %.2f): wrong password %v, unknown user %v",
			ratio, wrongPassDuration, noUserDuration)
	}
}

func TestLogout_RevokesToken(t *testing.T) {
	tokenRepo := newFakeTokenRepo()
	r := setupAuthRouter(newFakeUserRepo(), tokenRepo)

	w := performRequest(r, "POST", "/auth/logout", "")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	// authAs sets "test-jti"; logout must add it to the revocation list
	if !tokenRepo.revoked["test-jti"] {
		t.Fatalf("token was not added to the revocation list")
	}
}
