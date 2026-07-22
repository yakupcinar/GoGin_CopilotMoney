package handlers

// Bu dosya "beklenmeyen altyapı hatası" (DB çöktü, bağlantı koptu vb.)
// senaryolarını test eder. Fake repo'lara errBoom enjekte edilir ve
// handler'ın 500 döndürmesi + hata detayını client'a SIZDIRMAMASI beklenir.
//
// Bu yollar gerçek veritabanıyla test edilemez: Postgres'i test sırasında
// bilerek çökertmek gerekirdi. Fake repo ile tek satırda simüle ediliyor —
// dependency injection'ın en pratik faydalarından biri budur.

import (
	"GoGinMoneyCopilot/models"
	"net/http"
	"strings"
	"testing"
)

// assert500 hem status kodunu hem de hata detayının sızmadığını doğrular.
func assert500(t *testing.T, code int, body string) {
	t.Helper()
	if code != http.StatusInternalServerError {
		t.Fatalf("beklenen 500, gelen %d (body: %s)", code, body)
	}
	if strings.Contains(body, errBoom.Error()) {
		t.Fatalf("iç hata detayı client'a sızdı: %s", body)
	}
	if !strings.Contains(body, "Internal server error") {
		t.Fatalf("beklenen jenerik hata mesajı yok: %s", body)
	}
}

// ---- account 500 yolları ----

func TestCreateAccount_RepoErrorReturns500(t *testing.T) {
	repo := newFakeAccountRepo()
	repo.failOn("Create", errBoom)
	r := setupAccountRouter(repo, 1, models.RoleClient)

	w := performRequest(r, "POST", "/accounts", `{"name":"Ana Hesap"}`)

	assert500(t, w.Code, w.Body.String())
}

func TestGetAccount_RepoErrorReturns500(t *testing.T) {
	repo := newFakeAccountRepo()
	repo.seed(&models.Account{ID: 1, Name: "Hesap", UserID: 1})
	repo.failOn("GetByIDForUser", errBoom)
	r := setupAccountRouter(repo, 1, models.RoleClient)

	w := performRequest(r, "GET", "/accounts/1", "")

	assert500(t, w.Code, w.Body.String())
}

// Admin yolu farklı repo metodunu (GetByID) kullanır; o da 500 vermeli.
func TestGetAccount_AdminRepoErrorReturns500(t *testing.T) {
	repo := newFakeAccountRepo()
	repo.seed(&models.Account{ID: 1, Name: "Hesap", UserID: 1})
	repo.failOn("GetByID", errBoom)
	r := setupAccountRouter(repo, 99, models.RoleAdmin)

	w := performRequest(r, "GET", "/accounts/1", "")

	assert500(t, w.Code, w.Body.String())
}

func TestUpdateAccount_RepoErrorReturns500(t *testing.T) {
	repo := newFakeAccountRepo()
	repo.seed(&models.Account{ID: 1, Name: "Hesap", UserID: 1})
	repo.failOn("Update", errBoom) // sahiplik kontrolü geçer, güncelleme patlar
	r := setupAccountRouter(repo, 1, models.RoleClient)

	w := performRequest(r, "PUT", "/accounts/1", `{"name":"Yeni Ad"}`)

	assert500(t, w.Code, w.Body.String())
}

func TestDeleteAccount_RepoErrorReturns500(t *testing.T) {
	repo := newFakeAccountRepo()
	repo.seed(&models.Account{ID: 1, Name: "Hesap", UserID: 1})
	repo.failOn("Delete", errBoom)
	r := setupAccountRouter(repo, 1, models.RoleClient)

	w := performRequest(r, "DELETE", "/accounts/1", "")

	assert500(t, w.Code, w.Body.String())
	if len(repo.accounts) != 1 {
		t.Fatalf("hata durumunda hesap silinmiş görünüyor")
	}
}

// ---- category 500 yolları ----

func TestCreateCategory_RepoErrorReturns500(t *testing.T) {
	repo := newFakeCategoryRepo()
	repo.failOn("Create", errBoom)
	r := setupCategoryRouter(repo, 1, models.RoleClient)

	w := performRequest(r, "POST", "/categories", `{"name":"Maas","type":"income"}`)

	assert500(t, w.Code, w.Body.String())
}

func TestListCategories_RepoErrorReturns500(t *testing.T) {
	repo := newFakeCategoryRepo()
	repo.failOn("GetForUser", errBoom)
	r := setupCategoryRouter(repo, 1, models.RoleClient)

	w := performRequest(r, "GET", "/categories", "")

	assert500(t, w.Code, w.Body.String())
}

func TestUpdateCategory_FetchErrorReturns500(t *testing.T) {
	repo := newFakeCategoryRepo()
	repo.seed(&models.Category{ID: 1, Name: "Kategori", Type: "income", UserID: intPtr(1)})
	repo.failOn("GetByID", errBoom)
	r := setupCategoryRouter(repo, 1, models.RoleClient)

	w := performRequest(r, "PUT", "/categories/1", `{"name":"Yeni","type":"income"}`)

	assert500(t, w.Code, w.Body.String())
}

func TestUpdateCategory_UpdateErrorReturns500(t *testing.T) {
	repo := newFakeCategoryRepo()
	repo.seed(&models.Category{ID: 1, Name: "Kategori", Type: "income", UserID: intPtr(1)})
	repo.failOn("Update", errBoom) // yetki kontrolü geçer, güncelleme patlar
	r := setupCategoryRouter(repo, 1, models.RoleClient)

	w := performRequest(r, "PUT", "/categories/1", `{"name":"Yeni","type":"income"}`)

	assert500(t, w.Code, w.Body.String())
}

func TestDeleteCategory_RepoErrorReturns500(t *testing.T) {
	repo := newFakeCategoryRepo()
	repo.seed(&models.Category{ID: 1, Name: "Kategori", Type: "income", UserID: intPtr(1)})
	repo.failOn("Delete", errBoom)
	r := setupCategoryRouter(repo, 1, models.RoleClient)

	w := performRequest(r, "DELETE", "/categories/1", "")

	assert500(t, w.Code, w.Body.String())
}

// ---- transaction 500 yolları ----

func TestCreateTransaction_RepoErrorReturns500(t *testing.T) {
	accRepo := newFakeAccountRepo()
	accRepo.seed(&models.Account{ID: 1, Name: "Hesap", UserID: 1})
	txRepo := newFakeTransactionRepo()
	txRepo.failOn("Create", errBoom) // hesap sahipliği doğrulanır, kayıt patlar
	r := setupTransactionRouter(txRepo, accRepo, 1, models.RoleClient)

	body := `{"account_id":1,"category_id":1,"amount":10,"type":"income","transaction_date":"2026-07-13T00:00:00Z"}`
	w := performRequest(r, "POST", "/transactions", body)

	assert500(t, w.Code, w.Body.String())
}

// Transaction akışında hesabı çeken repo patlarsa da 500 dönmeli.
func TestCreateTransaction_AccountRepoErrorReturns500(t *testing.T) {
	accRepo := newFakeAccountRepo()
	accRepo.seed(&models.Account{ID: 1, Name: "Hesap", UserID: 1})
	accRepo.failOn("GetByIDForUser", errBoom)
	txRepo := newFakeTransactionRepo()
	r := setupTransactionRouter(txRepo, accRepo, 1, models.RoleClient)

	body := `{"account_id":1,"category_id":1,"amount":10,"type":"income","transaction_date":"2026-07-13T00:00:00Z"}`
	w := performRequest(r, "POST", "/transactions", body)

	assert500(t, w.Code, w.Body.String())
	if len(txRepo.transactions) != 0 {
		t.Fatalf("hesap doğrulanamadan transaction oluşturuldu")
	}
}

func TestGetTransaction_RepoErrorReturns500(t *testing.T) {
	accRepo := newFakeAccountRepo()
	accRepo.seed(&models.Account{ID: 1, Name: "Hesap", UserID: 1})
	txRepo := newFakeTransactionRepo()
	txRepo.failOn("GetByID", errBoom)
	r := setupTransactionRouter(txRepo, accRepo, 1, models.RoleClient)

	w := performRequest(r, "GET", "/transactions/1", "")

	assert500(t, w.Code, w.Body.String())
}

func TestListAccountTransactions_RepoErrorReturns500(t *testing.T) {
	accRepo := newFakeAccountRepo()
	accRepo.seed(&models.Account{ID: 1, Name: "Hesap", UserID: 1})
	txRepo := newFakeTransactionRepo()
	txRepo.failOn("ListByAccount", errBoom)
	r := setupTransactionRouter(txRepo, accRepo, 1, models.RoleClient)

	w := performRequest(r, "GET", "/accounts/1/transactions", "")

	assert500(t, w.Code, w.Body.String())
}

func TestUpdateTransaction_RepoErrorReturns500(t *testing.T) {
	accRepo := newFakeAccountRepo()
	accRepo.seed(&models.Account{ID: 1, Name: "Hesap", UserID: 1})
	txRepo := newFakeTransactionRepo()
	txRepo.seed(&models.Transaction{ID: 1, AccountID: 1, Amount: 10, Type: "income"})
	txRepo.failOn("Update", errBoom)
	r := setupTransactionRouter(txRepo, accRepo, 1, models.RoleClient)

	body := `{"category_id":2,"amount":50,"type":"expense","transaction_date":"2026-07-14T00:00:00Z"}`
	w := performRequest(r, "PUT", "/transactions/1", body)

	assert500(t, w.Code, w.Body.String())
}

func TestDeleteTransaction_RepoErrorReturns500(t *testing.T) {
	accRepo := newFakeAccountRepo()
	accRepo.seed(&models.Account{ID: 1, Name: "Hesap", UserID: 1})
	txRepo := newFakeTransactionRepo()
	txRepo.seed(&models.Transaction{ID: 1, AccountID: 1, Amount: 10, Type: "income"})
	txRepo.failOn("Delete", errBoom)
	r := setupTransactionRouter(txRepo, accRepo, 1, models.RoleClient)

	w := performRequest(r, "DELETE", "/transactions/1", "")

	assert500(t, w.Code, w.Body.String())
	if len(txRepo.transactions) != 1 {
		t.Fatalf("hata durumunda transaction silinmiş görünüyor")
	}
}

// ---- auth 500 yolları ----

func TestRegister_RepoErrorReturns500(t *testing.T) {
	userRepo := newFakeUserRepo()
	userRepo.failOn("Create", errBoom)
	r := setupAuthRouter(userRepo, newFakeTokenRepo())

	w := performRequest(r, "POST", "/register", `{"username":"kullanici","password":"gizlisifre123"}`)

	assert500(t, w.Code, w.Body.String())
}

// DB patladığında login "şifre yanlış" (401) DEĞİL, 500 dönmeli:
// altyapı arızası kimlik doğrulama hatası gibi görünmemeli.
func TestLogin_RepoErrorReturns500(t *testing.T) {
	userRepo := newFakeUserRepo()
	userRepo.failOn("GetByUsername", errBoom)
	r := setupAuthRouter(userRepo, newFakeTokenRepo())

	w := performRequest(r, "POST", "/login", `{"username":"kullanici","password":"herhangibirsifre"}`)

	assert500(t, w.Code, w.Body.String())
}

func TestLogout_RepoErrorReturns500(t *testing.T) {
	tokenRepo := newFakeTokenRepo()
	tokenRepo.failOn("Revoke", errBoom)
	r := setupAuthRouter(newFakeUserRepo(), tokenRepo)

	w := performRequest(r, "POST", "/auth/logout", "")

	assert500(t, w.Code, w.Body.String())
	if tokenRepo.revoked["test-jti"] {
		t.Fatalf("revoke başarısızken token iptal edilmiş görünüyor")
	}
}

// Bilinen domain hataları 500'e karışmamalı: repo ErrUserNotFound dönerse
// login yine 401 vermeli (regresyon koruması).
func TestLogin_UnknownUserStill401(t *testing.T) {
	r := setupAuthRouter(newFakeUserRepo(), newFakeTokenRepo())

	w := performRequest(r, "POST", "/login", `{"username":"olmayan","password":"herhangibirsifre"}`)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("beklenen 401, gelen %d (body: %s)", w.Code, w.Body.String())
	}
}
