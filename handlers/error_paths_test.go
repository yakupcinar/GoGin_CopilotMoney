package handlers

// This file tests "unexpected infrastructure failure" scenarios (the DB went
// down, the connection dropped, and so on). errBoom is injected into the fake
// repositories and the handler is expected to return 500 WITHOUT leaking the
// error detail to the client.
//
// These paths cannot be exercised against a real database: it would mean
// deliberately crashing Postgres mid-test. A fake repo simulates it in a
// single line — one of the most practical benefits of dependency injection.

import (
	"GoGinMoneyCopilot/models"
	"net/http"
	"strings"
	"testing"
	"time"
)

// assert500 verifies both the status code and that no error detail leaked.
func assert500(t *testing.T, code int, body string) {
	t.Helper()
	if code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d (body: %s)", code, body)
	}
	if strings.Contains(body, errBoom.Error()) {
		t.Fatalf("internal error detail leaked to the client: %s", body)
	}
	if !strings.Contains(body, "Internal server error") {
		t.Fatalf("the expected generic error message is missing: %s", body)
	}
}

// ---- account 500 paths ----

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

// The admin path uses a different repo method (GetByID); it must also 500.
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
	repo.failOn("Update", errBoom) // the ownership check passes, the update fails
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
		t.Fatalf("the account appears to have been deleted despite the error")
	}
}

// ---- category 500 paths ----

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
	repo.failOn("Update", errBoom) // the authorization check passes, the update fails
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

// ---- transaction 500 paths ----

func TestCreateTransaction_RepoErrorReturns500(t *testing.T) {
	accRepo := newFakeAccountRepo()
	accRepo.seed(&models.Account{ID: 1, Name: "Hesap", UserID: 1})
	txRepo := newFakeTransactionRepo()
	txRepo.failOn("Create", errBoom) // account ownership is verified, the insert fails
	r := setupTransactionRouter(txRepo, accRepo, 1, models.RoleClient)

	body := `{"account_id":1,"category_id":1,"amount":10,"type":"income","transaction_date":"2026-07-13T00:00:00Z"}`
	w := performRequest(r, "POST", "/transactions", body)

	assert500(t, w.Code, w.Body.String())
}

// If the repo that loads the account fails during the transaction flow, the
// response must also be 500.
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
		t.Fatalf("a transaction was created before the account could be verified")
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
	txRepo.failOn("ListByAccountPaged", errBoom)
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
		t.Fatalf("the transaction appears to have been deleted despite the error")
	}
}

// ---- auth 500 paths ----

func TestRegister_RepoErrorReturns500(t *testing.T) {
	userRepo := newFakeUserRepo()
	userRepo.failOn("Create", errBoom)
	r := setupAuthRouter(userRepo, newFakeTokenRepo())

	w := performRequest(r, "POST", "/register", `{"username":"kullanici","password":"gizlisifre123"}`)

	assert500(t, w.Code, w.Body.String())
}

// When the DB fails, login must return 500 — NOT "wrong password" (401):
// an infrastructure failure must not masquerade as an authentication error.
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
		t.Fatalf("the token appears revoked even though revoke failed")
	}
}

// Known domain errors must not be conflated with 500: if the repo returns
// ErrUserNotFound, login must still answer 401 (regression guard).
func TestLogin_UnknownUserStill401(t *testing.T) {
	r := setupAuthRouter(newFakeUserRepo(), newFakeTokenRepo())

	w := performRequest(r, "POST", "/login", `{"username":"olmayan","password":"herhangibirsifre"}`)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d (body: %s)", w.Code, w.Body.String())
	}
}

// ---- budget 500 paths ----

// validBudgetBody — a valid body with a single category.
const validBudgetBody = `{"name":"Aylık","start_date":"2026-01-05","period_days":30,"categories":[{"category_id":3,"limit_amount":6000}]}`

func seedBudgetCategory(catRepo *fakeCategoryRepo) {
	catRepo.seed(&models.Category{ID: 3, Name: "Market", Type: "expense", UserID: intPtr(1)})
}

func TestCreateBudget_RepoErrorReturns500(t *testing.T) {
	bRepo := newFakeBudgetRepo()
	bRepo.failOn("Create", errBoom)
	catRepo := newFakeCategoryRepo()
	seedBudgetCategory(catRepo)
	r := setupBudgetRouter(bRepo, catRepo, newFakeAccountRepo(), newFakeTransactionRepo(), 1, models.RoleClient)

	w := performRequest(r, "POST", "/budgets", validBudgetBody)
	assert500(t, w.Code, w.Body.String())
	if len(bRepo.budgets) != 0 {
		t.Fatalf("a budget was created despite the error")
	}
}

func TestCreateBudget_CategoryRepoErrorReturns500(t *testing.T) {
	bRepo := newFakeBudgetRepo()
	catRepo := newFakeCategoryRepo()
	seedBudgetCategory(catRepo)
	catRepo.failOn("GetForUser", errBoom)
	r := setupBudgetRouter(bRepo, catRepo, newFakeAccountRepo(), newFakeTransactionRepo(), 1, models.RoleClient)

	w := performRequest(r, "POST", "/budgets", validBudgetBody)
	assert500(t, w.Code, w.Body.String())
	if len(bRepo.budgets) != 0 {
		t.Fatalf("a budget was created before the category could be verified")
	}
}

// seededBudgetRouter — sets up a ready budget + account for the GET paths.
func seededBudgetRouter(t *testing.T, bRepo *fakeBudgetRepo, catRepo *fakeCategoryRepo, accRepo *fakeAccountRepo, txRepo *fakeTransactionRepo) {
	t.Helper()
	seedBudgetCategory(catRepo)
	accRepo.seed(&models.Account{ID: 1, Name: "Nakit", UserID: 1})
	bRepo.seed(&models.Budget{ID: 1, UserID: 1, Name: "Aylık", StartDate: models.CivilDate(time.Now().AddDate(0, 0, -5)), PeriodDays: 30},
		[]models.BudgetCategory{{ID: 1, BudgetID: 1, CategoryID: 3, LimitAmount: 6000}})
}

func TestGetBudget_RepoErrorReturns500(t *testing.T) {
	bRepo := newFakeBudgetRepo()
	bRepo.failOn("GetForUser", errBoom)
	r := setupBudgetRouter(bRepo, newFakeCategoryRepo(), newFakeAccountRepo(), newFakeTransactionRepo(), 1, models.RoleClient)

	w := performRequest(r, "GET", "/budgets", "")
	assert500(t, w.Code, w.Body.String())
}

func TestGetBudget_ListCategoriesErrorReturns500(t *testing.T) {
	bRepo := newFakeBudgetRepo()
	catRepo := newFakeCategoryRepo()
	accRepo := newFakeAccountRepo()
	txRepo := newFakeTransactionRepo()
	seededBudgetRouter(t, bRepo, catRepo, accRepo, txRepo)
	bRepo.failOn("ListCategories", errBoom)
	r := setupBudgetRouter(bRepo, catRepo, accRepo, txRepo, 1, models.RoleClient)

	w := performRequest(r, "GET", "/budgets", "")
	assert500(t, w.Code, w.Body.String())
}

func TestGetBudget_AccountRepoErrorReturns500(t *testing.T) {
	bRepo := newFakeBudgetRepo()
	catRepo := newFakeCategoryRepo()
	accRepo := newFakeAccountRepo()
	txRepo := newFakeTransactionRepo()
	seededBudgetRouter(t, bRepo, catRepo, accRepo, txRepo)
	accRepo.failOn("ListForUser", errBoom)
	r := setupBudgetRouter(bRepo, catRepo, accRepo, txRepo, 1, models.RoleClient)

	w := performRequest(r, "GET", "/budgets", "")
	assert500(t, w.Code, w.Body.String())
}

func TestGetBudget_SumErrorReturns500(t *testing.T) {
	bRepo := newFakeBudgetRepo()
	catRepo := newFakeCategoryRepo()
	accRepo := newFakeAccountRepo()
	txRepo := newFakeTransactionRepo()
	seededBudgetRouter(t, bRepo, catRepo, accRepo, txRepo)
	txRepo.failOn("SumExpenseByCategory", errBoom)
	r := setupBudgetRouter(bRepo, catRepo, accRepo, txRepo, 1, models.RoleClient)

	w := performRequest(r, "GET", "/budgets", "")
	assert500(t, w.Code, w.Body.String())
}

func TestUpdateBudget_RepoErrorReturns500(t *testing.T) {
	bRepo := newFakeBudgetRepo()
	catRepo := newFakeCategoryRepo()
	seedBudgetCategory(catRepo)
	bRepo.seed(&models.Budget{ID: 1, UserID: 1, Name: "Var", StartDate: models.CivilDate(time.Now()), PeriodDays: 30}, nil)
	bRepo.failOn("Replace", errBoom)
	r := setupBudgetRouter(bRepo, catRepo, newFakeAccountRepo(), newFakeTransactionRepo(), 1, models.RoleClient)

	w := performRequest(r, "PUT", "/budgets", validBudgetBody)
	assert500(t, w.Code, w.Body.String())
}

func TestDeleteBudget_RepoErrorReturns500(t *testing.T) {
	bRepo := newFakeBudgetRepo()
	bRepo.seed(&models.Budget{ID: 1, UserID: 1, Name: "Var", StartDate: models.CivilDate(time.Now()), PeriodDays: 30}, nil)
	bRepo.failOn("Delete", errBoom)
	r := setupBudgetRouter(bRepo, newFakeCategoryRepo(), newFakeAccountRepo(), newFakeTransactionRepo(), 1, models.RoleClient)

	w := performRequest(r, "DELETE", "/budgets", "")
	assert500(t, w.Code, w.Body.String())
	if len(bRepo.budgets) != 1 {
		t.Fatalf("the budget was deleted despite the error")
	}
}

func TestDeleteCategory_BudgetCountErrorReturns500(t *testing.T) {
	repo := newFakeCategoryRepo()
	repo.seed(&models.Category{ID: 3, Name: "Market", Type: "expense", UserID: intPtr(1)})
	bRepo := newFakeBudgetRepo()
	bRepo.failOn("CountByCategory", errBoom)
	r := setupCategoryRouterWithBudgets(repo, bRepo, 1, models.RoleClient)

	w := performRequest(r, "DELETE", "/categories/3", "")
	assert500(t, w.Code, w.Body.String())
	if _, ok := repo.categories[3]; !ok {
		t.Fatalf("the category was deleted even though the budget count failed")
	}
}
