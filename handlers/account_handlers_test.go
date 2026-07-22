package handlers

import (
	"GoGinMoneyCopilot/models"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// setupAccountRouter, verilen fake repo ve giriş yapmış kullanıcı ile
// account route'larını kuran bir gin engine döndürür.
func setupAccountRouter(repo *fakeAccountRepo, userID int, role models.Role) *gin.Engine {
	h := NewAccountHandler(repo)
	r := gin.New()
	r.Use(authAs(userID, role))
	r.POST("/accounts", h.CreateAccount)
	r.GET("/accounts/:id", h.GetAccount)
	r.PUT("/accounts/:id", h.UpdateAccount)
	r.DELETE("/accounts/:id", h.DeleteAccount)
	return r
}

func TestCreateAccount_Success(t *testing.T) {
	repo := newFakeAccountRepo()
	r := setupAccountRouter(repo, 1, models.RoleClient)

	w := performRequest(r, "POST", "/accounts", `{"name":"Ana Hesap"}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("beklenen 201, gelen %d (body: %s)", w.Code, w.Body.String())
	}
	if len(repo.accounts) != 1 {
		t.Fatalf("repo'da 1 hesap bekleniyordu, %d var", len(repo.accounts))
	}
}

func TestCreateAccount_InvalidInput(t *testing.T) {
	repo := newFakeAccountRepo()
	r := setupAccountRouter(repo, 1, models.RoleClient)

	// boş isim -> binding "required" başarısız -> 400
	w := performRequest(r, "POST", "/accounts", `{"name":""}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("beklenen 400, gelen %d", w.Code)
	}
	if len(repo.accounts) != 0 {
		t.Fatalf("geçersiz input'ta hesap oluşmamalıydı")
	}
}

func TestGetAccount_OwnerCanRead(t *testing.T) {
	repo := newFakeAccountRepo()
	repo.seed(&models.Account{ID: 1, Name: "Benim Hesap", UserID: 1})
	r := setupAccountRouter(repo, 1, models.RoleClient)

	w := performRequest(r, "GET", "/accounts/1", "")

	if w.Code != http.StatusOK {
		t.Fatalf("beklenen 200, gelen %d", w.Code)
	}
}

func TestGetAccount_NotFound(t *testing.T) {
	repo := newFakeAccountRepo()
	r := setupAccountRouter(repo, 1, models.RoleClient)

	w := performRequest(r, "GET", "/accounts/999", "")

	if w.Code != http.StatusNotFound {
		t.Fatalf("beklenen 404, gelen %d", w.Code)
	}
}

// GÜVENLİK: başka kullanıcının hesabına erişim, hesabın varlığını bile
// sızdırmadan 404 dönmeli (query-level ownership scoping).
func TestGetAccount_OwnershipIsolation(t *testing.T) {
	repo := newFakeAccountRepo()
	repo.seed(&models.Account{ID: 1, Name: "User1 Hesap", UserID: 1})

	// user 2, user 1'in hesabını okumaya çalışıyor
	r := setupAccountRouter(repo, 2, models.RoleClient)
	w := performRequest(r, "GET", "/accounts/1", "")

	if w.Code != http.StatusNotFound {
		t.Fatalf("başka kullanıcının hesabı için 404 bekleniyordu, gelen %d", w.Code)
	}
}

// Admin, herhangi bir kullanıcının hesabını görebilmeli.
func TestGetAccount_AdminCanReadAny(t *testing.T) {
	repo := newFakeAccountRepo()
	repo.seed(&models.Account{ID: 1, Name: "User1 Hesap", UserID: 1})

	r := setupAccountRouter(repo, 99, models.RoleAdmin)
	w := performRequest(r, "GET", "/accounts/1", "")

	if w.Code != http.StatusOK {
		t.Fatalf("admin için 200 bekleniyordu, gelen %d", w.Code)
	}
}

func TestUpdateAccount_OwnerCanUpdate(t *testing.T) {
	repo := newFakeAccountRepo()
	repo.seed(&models.Account{ID: 1, Name: "Eski Ad", UserID: 1})
	r := setupAccountRouter(repo, 1, models.RoleClient)

	w := performRequest(r, "PUT", "/accounts/1", `{"name":"Yeni Ad"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("beklenen 200, gelen %d", w.Code)
	}
	if repo.accounts[1].Name != "Yeni Ad" {
		t.Fatalf("isim güncellenmedi: %q", repo.accounts[1].Name)
	}
}

// GÜVENLİK: başka kullanıcı güncelleme deneyince 404 ve veri değişmemeli.
func TestUpdateAccount_OwnershipIsolation(t *testing.T) {
	repo := newFakeAccountRepo()
	repo.seed(&models.Account{ID: 1, Name: "Dokunma", UserID: 1})

	r := setupAccountRouter(repo, 2, models.RoleClient)
	w := performRequest(r, "PUT", "/accounts/1", `{"name":"Hacklendi"}`)

	if w.Code != http.StatusNotFound {
		t.Fatalf("beklenen 404, gelen %d", w.Code)
	}
	if repo.accounts[1].Name != "Dokunma" {
		t.Fatalf("başka kullanıcı veriyi değiştirdi: %q", repo.accounts[1].Name)
	}
}

func TestDeleteAccount_OwnerCanDelete(t *testing.T) {
	repo := newFakeAccountRepo()
	repo.seed(&models.Account{ID: 1, Name: "Silinecek", UserID: 1})
	r := setupAccountRouter(repo, 1, models.RoleClient)

	w := performRequest(r, "DELETE", "/accounts/1", "")

	if w.Code != http.StatusOK {
		t.Fatalf("beklenen 200, gelen %d", w.Code)
	}
	if len(repo.accounts) != 0 {
		t.Fatalf("hesap silinmedi")
	}
}

func TestDeleteAccount_OwnershipIsolation(t *testing.T) {
	repo := newFakeAccountRepo()
	repo.seed(&models.Account{ID: 1, Name: "Kalıcı", UserID: 1})

	r := setupAccountRouter(repo, 2, models.RoleClient)
	w := performRequest(r, "DELETE", "/accounts/1", "")

	if w.Code != http.StatusNotFound {
		t.Fatalf("beklenen 404, gelen %d", w.Code)
	}
	if len(repo.accounts) != 1 {
		t.Fatalf("başka kullanıcı hesabı sildi")
	}
}

func TestGetAccount_InvalidIDFormat(t *testing.T) {
	repo := newFakeAccountRepo()
	r := setupAccountRouter(repo, 1, models.RoleClient)

	w := performRequest(r, "GET", "/accounts/abc", "")

	if w.Code != http.StatusBadRequest {
		t.Fatalf("beklenen 400, gelen %d", w.Code)
	}
}

// İçinde işlem olan hesap silinemez: 409 dönmeli, 500 DEĞİL.
//
// Gözlemlenen gerçek hata: repository foreign key ihlalini (23503) jenerik
// hataya çeviriyordu, client "Internal server error" (500) alıyordu.
// Veri güvendeydi (DB engelliyor) ama kullanıcı "sunucu bozuk" sanıyordu.
func TestDeleteAccount_WithTransactionsReturnsConflict(t *testing.T) {
	repo := newFakeAccountRepo()
	repo.seed(&models.Account{ID: 1, Name: "Dolu Hesap", UserID: 1})
	repo.inUse[1] = true
	r := setupAccountRouter(repo, 1, models.RoleClient)

	w := performRequest(r, "DELETE", "/accounts/1", "")

	if w.Code != http.StatusConflict {
		t.Fatalf("beklenen 409, gelen %d (body: %s)", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "Internal server error") {
		t.Fatalf("çakışma durumu sunucu hatası gibi gösterildi: %s", w.Body.String())
	}
	if len(repo.accounts) != 1 {
		t.Fatalf("hesap silinmiş görünüyor")
	}
}
