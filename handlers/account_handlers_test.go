package handlers

import (
	"GoGinMoneyCopilot/models"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// setupAccountRouter returns a gin engine wired with the given fake repo
// and an authenticated user, registering the account routes.
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
		t.Fatalf("expected 201, got %d (body: %s)", w.Code, w.Body.String())
	}
	if len(repo.accounts) != 1 {
		t.Fatalf("expected 1 account in the repo, found %d", len(repo.accounts))
	}
}

func TestCreateAccount_InvalidInput(t *testing.T) {
	repo := newFakeAccountRepo()
	r := setupAccountRouter(repo, 1, models.RoleClient)

	// empty name -> the "required" binding fails -> 400
	w := performRequest(r, "POST", "/accounts", `{"name":""}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if len(repo.accounts) != 0 {
		t.Fatalf("no account should have been created for invalid input")
	}
}

func TestGetAccount_OwnerCanRead(t *testing.T) {
	repo := newFakeAccountRepo()
	repo.seed(&models.Account{ID: 1, Name: "Benim Hesap", UserID: 1})
	r := setupAccountRouter(repo, 1, models.RoleClient)

	w := performRequest(r, "GET", "/accounts/1", "")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestGetAccount_NotFound(t *testing.T) {
	repo := newFakeAccountRepo()
	r := setupAccountRouter(repo, 1, models.RoleClient)

	w := performRequest(r, "GET", "/accounts/999", "")

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// SECURITY: accessing another user's account must return 404 without even
// leaking the account's existence (query-level ownership scoping).
func TestGetAccount_OwnershipIsolation(t *testing.T) {
	repo := newFakeAccountRepo()
	repo.seed(&models.Account{ID: 1, Name: "User1 Hesap", UserID: 1})

	// user 2 attempts to read user 1's account
	r := setupAccountRouter(repo, 2, models.RoleClient)
	w := performRequest(r, "GET", "/accounts/1", "")

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for another user's account, got %d", w.Code)
	}
}

// An admin must be able to read any user's account.
func TestGetAccount_AdminCanReadAny(t *testing.T) {
	repo := newFakeAccountRepo()
	repo.seed(&models.Account{ID: 1, Name: "User1 Hesap", UserID: 1})

	r := setupAccountRouter(repo, 99, models.RoleAdmin)
	w := performRequest(r, "GET", "/accounts/1", "")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin, got %d", w.Code)
	}
}

func TestUpdateAccount_OwnerCanUpdate(t *testing.T) {
	repo := newFakeAccountRepo()
	repo.seed(&models.Account{ID: 1, Name: "Eski Ad", UserID: 1})
	r := setupAccountRouter(repo, 1, models.RoleClient)

	w := performRequest(r, "PUT", "/accounts/1", `{"name":"Yeni Ad"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if repo.accounts[1].Name != "Yeni Ad" {
		t.Fatalf("name was not updated: %q", repo.accounts[1].Name)
	}
}

// SECURITY: when another user attempts an update the response must be 404
// and the data must remain unchanged.
func TestUpdateAccount_OwnershipIsolation(t *testing.T) {
	repo := newFakeAccountRepo()
	repo.seed(&models.Account{ID: 1, Name: "Dokunma", UserID: 1})

	r := setupAccountRouter(repo, 2, models.RoleClient)
	w := performRequest(r, "PUT", "/accounts/1", `{"name":"Hacklendi"}`)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if repo.accounts[1].Name != "Dokunma" {
		t.Fatalf("another user modified the data: %q", repo.accounts[1].Name)
	}
}

func TestDeleteAccount_OwnerCanDelete(t *testing.T) {
	repo := newFakeAccountRepo()
	repo.seed(&models.Account{ID: 1, Name: "Silinecek", UserID: 1})
	r := setupAccountRouter(repo, 1, models.RoleClient)

	w := performRequest(r, "DELETE", "/accounts/1", "")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(repo.accounts) != 0 {
		t.Fatalf("account was not deleted")
	}
}

func TestDeleteAccount_OwnershipIsolation(t *testing.T) {
	repo := newFakeAccountRepo()
	repo.seed(&models.Account{ID: 1, Name: "Kalıcı", UserID: 1})

	r := setupAccountRouter(repo, 2, models.RoleClient)
	w := performRequest(r, "DELETE", "/accounts/1", "")

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if len(repo.accounts) != 1 {
		t.Fatalf("another user deleted the account")
	}
}

func TestGetAccount_InvalidIDFormat(t *testing.T) {
	repo := newFakeAccountRepo()
	r := setupAccountRouter(repo, 1, models.RoleClient)

	w := performRequest(r, "GET", "/accounts/abc", "")

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// An account that still holds transactions cannot be deleted: the response
// must be 409, NOT 500.
//
// Real defect observed: the repository mapped the foreign-key violation
// (23503) to a generic error, so the client received "Internal server error"
// (500). The data was safe (the DB blocked it) but the user was led to
// believe the server was broken.
func TestDeleteAccount_WithTransactionsReturnsConflict(t *testing.T) {
	repo := newFakeAccountRepo()
	repo.seed(&models.Account{ID: 1, Name: "Dolu Hesap", UserID: 1})
	repo.inUse[1] = true
	r := setupAccountRouter(repo, 1, models.RoleClient)

	w := performRequest(r, "DELETE", "/accounts/1", "")

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d (body: %s)", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "Internal server error") {
		t.Fatalf("a conflict was surfaced as a server error: %s", w.Body.String())
	}
	if len(repo.accounts) != 1 {
		t.Fatalf("the account appears to have been deleted")
	}
}
