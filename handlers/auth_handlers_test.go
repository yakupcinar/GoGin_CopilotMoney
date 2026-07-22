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

// setupAuthRouterFull — refresh repo'ya da erişmek isteyen testler için.
func setupAuthRouterFull(userRepo *fakeUserRepo, tokenRepo *fakeTokenRepo, refreshRepo *fakeRefreshRepo) *gin.Engine {
	h := NewAuthHandler(userRepo, tokenRepo, refreshRepo)
	r := gin.New()
	r.POST("/register", h.Register)
	r.POST("/login", h.Login)
	// /auth/refresh korumasız: access token'ın süresi dolduğu için buradayız.
	r.POST("/auth/refresh", h.Refresh)
	// logout korumalı: AuthMiddleware'in koyduğu değerleri authAs taklit eder
	r.POST("/auth/logout", authAs(1, models.RoleClient), h.Logout)
	return r
}

func TestRegister_Success(t *testing.T) {
	userRepo := newFakeUserRepo()
	r := setupAuthRouter(userRepo, newFakeTokenRepo())

	w := performRequest(r, "POST", "/register", `{"username":"yenikullanici","password":"gizlisifre123"}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("beklenen 201, gelen %d (body: %s)", w.Code, w.Body.String())
	}
	user, ok := userRepo.users["yenikullanici"]
	if !ok {
		t.Fatalf("kullanıcı repo'ya eklenmedi")
	}
	// Şifre düz metin olarak DEĞİL, hash'lenmiş saklanmalı
	if user.PasswordHash == "gizlisifre123" {
		t.Fatalf("şifre düz metin saklandı!")
	}
	if !auth.CheckPassword("gizlisifre123", user.PasswordHash) {
		t.Fatalf("saklanan hash şifreyle eşleşmiyor")
	}
	// Yeni kullanıcı varsayılan olarak client rolünde olmalı
	if user.Role != models.RoleClient {
		t.Fatalf("varsayılan rol client olmalıydı, gelen %q", user.Role)
	}
}

func TestRegister_DuplicateUsername(t *testing.T) {
	userRepo := newFakeUserRepo()
	userRepo.seedUser("mevcut", "hash", models.RoleClient)
	r := setupAuthRouter(userRepo, newFakeTokenRepo())

	w := performRequest(r, "POST", "/register", `{"username":"mevcut","password":"gizlisifre123"}`)

	if w.Code != http.StatusConflict {
		t.Fatalf("beklenen 409, gelen %d", w.Code)
	}
}

func TestRegister_ShortPasswordRejected(t *testing.T) {
	userRepo := newFakeUserRepo()
	r := setupAuthRouter(userRepo, newFakeTokenRepo())

	// binding min=8 -> 400
	w := performRequest(r, "POST", "/register", `{"username":"kullanici","password":"kisa"}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("beklenen 400, gelen %d", w.Code)
	}
	if len(userRepo.users) != 0 {
		t.Fatalf("geçersiz input'ta kullanıcı oluşturuldu")
	}
}

func TestLogin_Success(t *testing.T) {
	os.Setenv("JWT_SECRET", "test-secret")
	defer os.Unsetenv("JWT_SECRET")

	hash, err := auth.HashPassword("dogrusifre123")
	if err != nil {
		t.Fatalf("hash üretilemedi: %v", err)
	}
	userRepo := newFakeUserRepo()
	userRepo.seedUser("testuser", hash, models.RoleClient)
	r := setupAuthRouter(userRepo, newFakeTokenRepo())

	w := performRequest(r, "POST", "/login", `{"username":"testuser","password":"dogrusifre123"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("beklenen 200, gelen %d (body: %s)", w.Code, w.Body.String())
	}
	var resp models.LoginResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("cevap parse edilemedi: %v", err)
	}
	if resp.Token == "" {
		t.Fatalf("token boş döndü")
	}
	// Dönen token gerçekten doğrulanabilir olmalı ve doğru kullanıcıyı taşımalı
	claims, err := auth.ValidateToken(resp.Token)
	if err != nil {
		t.Fatalf("dönen token doğrulanamadı: %v", err)
	}
	if claims.UserID != userRepo.users["testuser"].ID {
		t.Fatalf("token yanlış user_id taşıyor: %d", claims.UserID)
	}
	if claims.Role != models.RoleClient {
		t.Fatalf("token yanlış rol taşıyor: %q", claims.Role)
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	hash, _ := auth.HashPassword("dogrusifre123")
	userRepo := newFakeUserRepo()
	userRepo.seedUser("testuser", hash, models.RoleClient)
	r := setupAuthRouter(userRepo, newFakeTokenRepo())

	w := performRequest(r, "POST", "/login", `{"username":"testuser","password":"yanlissifre"}`)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("beklenen 401, gelen %d", w.Code)
	}
}

func TestLogin_UnknownUser(t *testing.T) {
	r := setupAuthRouter(newFakeUserRepo(), newFakeTokenRepo())

	w := performRequest(r, "POST", "/login", `{"username":"olmayan","password":"herhangibirsifre"}`)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("beklenen 401, gelen %d", w.Code)
	}
}

// Kullanıcı adı var mı yok mu bilgisi hata mesajından sızmamalı:
// iki durumda da aynı mesaj dönmeli.
func TestLogin_SameErrorMessageForBothFailures(t *testing.T) {
	hash, _ := auth.HashPassword("dogrusifre123")
	userRepo := newFakeUserRepo()
	userRepo.seedUser("testuser", hash, models.RoleClient)
	r := setupAuthRouter(userRepo, newFakeTokenRepo())

	wrongPass := performRequest(r, "POST", "/login", `{"username":"testuser","password":"yanlis"}`)
	noUser := performRequest(r, "POST", "/login", `{"username":"olmayan","password":"yanlis"}`)

	if wrongPass.Body.String() != noUser.Body.String() {
		t.Fatalf("hata mesajları farklı, kullanıcı adı varlığı sızıyor:\n  yanlış şifre: %s\n  olmayan kullanıcı: %s",
			wrongPass.Body.String(), noUser.Body.String())
	}
}

// Login süresi: kullanıcı yokken de bcrypt çalıştırıldığı için (dummyHash),
// iki hata yolu arasında büyük zaman farkı olmamalı (timing side-channel koruması).
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
		t.Fatalf("iki hata yolu arasında şüpheli zaman farkı var (oran %.2f): yanlış şifre %v, olmayan kullanıcı %v",
			ratio, wrongPassDuration, noUserDuration)
	}
}

func TestLogout_RevokesToken(t *testing.T) {
	tokenRepo := newFakeTokenRepo()
	r := setupAuthRouter(newFakeUserRepo(), tokenRepo)

	w := performRequest(r, "POST", "/auth/logout", "")

	if w.Code != http.StatusOK {
		t.Fatalf("beklenen 200, gelen %d", w.Code)
	}
	// authAs "test-jti" değerini set ediyor; logout onu iptal listesine eklemeli
	if !tokenRepo.revoked["test-jti"] {
		t.Fatalf("token iptal listesine eklenmedi")
	}
}
