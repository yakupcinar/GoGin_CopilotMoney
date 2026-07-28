package handlers

import (
	"GoGinMoneyCopilot/models"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func setupBudgetRouter(bRepo *fakeBudgetRepo, catRepo *fakeCategoryRepo, accRepo *fakeAccountRepo, txRepo *fakeTransactionRepo, userID int, role models.Role) *gin.Engine {
	h := NewBudgetHandler(bRepo, catRepo, accRepo, txRepo)
	r := gin.New()
	r.Use(authAs(userID, role))
	r.POST("/budgets", h.CreateBudget)
	r.GET("/budgets", h.GetBudget)
	r.PUT("/budgets", h.UpdateBudget)
	r.DELETE("/budgets", h.DeleteBudget)
	return r
}

// seedExpenseCategories — market(3) and transport(9) expense categories for user 1.
func seedExpenseCategories(catRepo *fakeCategoryRepo, userID int) {
	u := userID
	catRepo.seed(&models.Category{ID: 3, Name: "Market", Type: "expense", UserID: &u})
	catRepo.seed(&models.Category{ID: 9, Name: "Ulaşım", Type: "expense", UserID: &u})
}

func moneyEq(a, b float64) bool { return math.Abs(a-b) < 0.001 }

// --- Create ---

func TestCreateBudget_Success(t *testing.T) {
	bRepo := newFakeBudgetRepo()
	catRepo := newFakeCategoryRepo()
	seedExpenseCategories(catRepo, 1)
	r := setupBudgetRouter(bRepo, catRepo, newFakeAccountRepo(), newFakeTransactionRepo(), 1, models.RoleClient)

	body := `{"name":"Aylık","start_date":"2026-01-05","period_days":30,"categories":[{"category_id":3,"limit_amount":6000},{"category_id":9,"limit_amount":2000}]}`
	w := performRequest(r, "POST", "/budgets", body)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (body: %s)", w.Code, w.Body.String())
	}
	if len(bRepo.budgets) != 1 {
		t.Fatalf("budget was not created")
	}
	b, _ := bRepo.GetForUser(context.Background(), 1)
	if len(bRepo.lines[b.ID]) != 2 {
		t.Fatalf("expected 2 category lines, got %d", len(bRepo.lines[b.ID]))
	}
	if !b.StartDate.Equal(models.CivilDate(time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC))) {
		t.Fatalf("wrong start date: %s", b.StartDate)
	}
}

func TestCreateBudget_InvalidInput(t *testing.T) {
	bRepo := newFakeBudgetRepo()
	catRepo := newFakeCategoryRepo()
	seedExpenseCategories(catRepo, 1)
	r := setupBudgetRouter(bRepo, catRepo, newFakeAccountRepo(), newFakeTransactionRepo(), 1, models.RoleClient)

	// period_days: 0 -> the binding fails
	body := `{"name":"X","start_date":"2026-01-05","period_days":0,"categories":[{"category_id":3,"limit_amount":6000}]}`
	w := performRequest(r, "POST", "/budgets", body)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if len(bRepo.budgets) != 0 {
		t.Fatalf("a budget was created with invalid input")
	}
}

func TestCreateBudget_PeriodDaysTooLarge(t *testing.T) {
	bRepo := newFakeBudgetRepo()
	catRepo := newFakeCategoryRepo()
	seedExpenseCategories(catRepo, 1)
	r := setupBudgetRouter(bRepo, catRepo, newFakeAccountRepo(), newFakeTransactionRepo(), 1, models.RoleClient)

	body := `{"name":"X","start_date":"2026-01-05","period_days":366,"categories":[{"category_id":3,"limit_amount":6000}]}`
	w := performRequest(r, "POST", "/budgets", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 (period_days exceeded 365), got %d", w.Code)
	}
}

func TestCreateBudget_EmptyCategoriesRejected(t *testing.T) {
	bRepo := newFakeBudgetRepo()
	catRepo := newFakeCategoryRepo()
	r := setupBudgetRouter(bRepo, catRepo, newFakeAccountRepo(), newFakeTransactionRepo(), 1, models.RoleClient)

	body := `{"name":"X","start_date":"2026-01-05","period_days":30,"categories":[]}`
	w := performRequest(r, "POST", "/budgets", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 (empty categories), got %d", w.Code)
	}
}

// Proves that "dive" is wired up: the 2nd element is malformed (category_id: 0).
func TestCreateBudget_MalformedCategoryLineRejected(t *testing.T) {
	bRepo := newFakeBudgetRepo()
	catRepo := newFakeCategoryRepo()
	seedExpenseCategories(catRepo, 1)
	r := setupBudgetRouter(bRepo, catRepo, newFakeAccountRepo(), newFakeTransactionRepo(), 1, models.RoleClient)

	body := `{"name":"X","start_date":"2026-01-05","period_days":30,"categories":[{"category_id":3,"limit_amount":6000},{"category_id":0,"limit_amount":100}]}`
	w := performRequest(r, "POST", "/budgets", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 (dive should catch the malformed line), got %d", w.Code)
	}
	if len(bRepo.budgets) != 0 {
		t.Fatalf("a budget was created with a malformed line")
	}
}

func TestCreateBudget_DuplicateCategoryRejected(t *testing.T) {
	bRepo := newFakeBudgetRepo()
	catRepo := newFakeCategoryRepo()
	seedExpenseCategories(catRepo, 1)
	r := setupBudgetRouter(bRepo, catRepo, newFakeAccountRepo(), newFakeTransactionRepo(), 1, models.RoleClient)

	body := `{"name":"X","start_date":"2026-01-05","period_days":30,"categories":[{"category_id":3,"limit_amount":6000},{"category_id":3,"limit_amount":2000}]}`
	w := performRequest(r, "POST", "/budgets", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 (duplicate category), got %d (body: %s)", w.Code, w.Body.String())
	}
	if len(bRepo.budgets) != 0 {
		t.Fatalf("a budget was created with a duplicate category")
	}
}

func TestCreateBudget_FutureStartDateRejected(t *testing.T) {
	bRepo := newFakeBudgetRepo()
	catRepo := newFakeCategoryRepo()
	seedExpenseCategories(catRepo, 1)
	r := setupBudgetRouter(bRepo, catRepo, newFakeAccountRepo(), newFakeTransactionRepo(), 1, models.RoleClient)

	body := `{"name":"X","start_date":"2999-01-01","period_days":30,"categories":[{"category_id":3,"limit_amount":6000}]}`
	w := performRequest(r, "POST", "/budgets", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 (future date), got %d", w.Code)
	}
	if len(bRepo.budgets) != 0 {
		t.Fatalf("a budget was created with a future date")
	}
}

func TestCreateBudget_IncomeCategoryRejected(t *testing.T) {
	bRepo := newFakeBudgetRepo()
	catRepo := newFakeCategoryRepo()
	u := 1
	catRepo.seed(&models.Category{ID: 5, Name: "Maaş", Type: "income", UserID: &u})
	r := setupBudgetRouter(bRepo, catRepo, newFakeAccountRepo(), newFakeTransactionRepo(), 1, models.RoleClient)

	body := `{"name":"X","start_date":"2026-01-05","period_days":30,"categories":[{"category_id":5,"limit_amount":6000}]}`
	w := performRequest(r, "POST", "/budgets", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 (an income category cannot be budgeted), got %d", w.Code)
	}
}

// SECURITY: another user's category cannot be added to a budget; 404 without leaking its existence.
func TestCreateBudget_ForeignCategoryReturns404(t *testing.T) {
	bRepo := newFakeBudgetRepo()
	catRepo := newFakeCategoryRepo()
	other := 2
	catRepo.seed(&models.Category{ID: 42, Name: "Gizli", Type: "expense", UserID: &other})
	r := setupBudgetRouter(bRepo, catRepo, newFakeAccountRepo(), newFakeTransactionRepo(), 1, models.RoleClient)

	body := `{"name":"X","start_date":"2026-01-05","period_days":30,"categories":[{"category_id":42,"limit_amount":6000}]}`
	w := performRequest(r, "POST", "/budgets", body)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if len(bRepo.budgets) != 0 {
		t.Fatalf("a budget was created with another user's category")
	}
}

func TestCreateBudget_GlobalCategoryAccepted(t *testing.T) {
	bRepo := newFakeBudgetRepo()
	catRepo := newFakeCategoryRepo()
	catRepo.seed(&models.Category{ID: 100, Name: "Fatura", Type: "expense", UserID: nil}) // global
	r := setupBudgetRouter(bRepo, catRepo, newFakeAccountRepo(), newFakeTransactionRepo(), 1, models.RoleClient)

	body := `{"name":"X","start_date":"2026-01-05","period_days":30,"categories":[{"category_id":100,"limit_amount":6000}]}`
	w := performRequest(r, "POST", "/budgets", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 (a global category should be accepted), got %d (body: %s)", w.Code, w.Body.String())
	}
}

func TestCreateBudget_SecondBudgetReturnsConflict(t *testing.T) {
	bRepo := newFakeBudgetRepo()
	bRepo.seed(&models.Budget{ID: 1, UserID: 1, Name: "Var", StartDate: models.CivilDate(time.Now()), PeriodDays: 30}, nil)
	catRepo := newFakeCategoryRepo()
	seedExpenseCategories(catRepo, 1)
	r := setupBudgetRouter(bRepo, catRepo, newFakeAccountRepo(), newFakeTransactionRepo(), 1, models.RoleClient)

	body := `{"name":"İkinci","start_date":"2026-01-05","period_days":30,"categories":[{"category_id":3,"limit_amount":6000}]}`
	w := performRequest(r, "POST", "/budgets", body)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 (one budget per user), got %d", w.Code)
	}
}

// --- Get ---

// currentPeriod — computes the same period the handler uses with its own
// `now`, making boundary tests independent of wall-clock time.
func currentPeriod(b *models.Budget) models.Period {
	return b.PeriodAt(time.Now().In(models.AppLocation()), 0)
}

func decodeBudgetView(t *testing.T, w *httptest.ResponseRecorder) models.BudgetView {
	t.Helper()
	var v models.BudgetView
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("response could not be decoded: %v (body: %s)", err, w.Body.String())
	}
	return v
}

func TestGetBudget_Success(t *testing.T) {
	bRepo := newFakeBudgetRepo()
	catRepo := newFakeCategoryRepo()
	seedExpenseCategories(catRepo, 1)
	accRepo := newFakeAccountRepo()
	accRepo.seed(&models.Account{ID: 1, Name: "Nakit", UserID: 1})
	txRepo := newFakeTransactionRepo()

	b := &models.Budget{ID: 1, UserID: 1, Name: "Aylık", StartDate: models.CivilDate(time.Now().AddDate(0, 0, -5)), PeriodDays: 30}
	bRepo.seed(b, []models.BudgetCategory{
		{ID: 1, BudgetID: 1, CategoryID: 3, LimitAmount: 6000},
		{ID: 2, BudgetID: 1, CategoryID: 9, LimitAmount: 2000},
	})
	p := currentPeriod(b)
	txRepo.seed(&models.Transaction{ID: 1, AccountID: 1, CategoryID: 3, Amount: 4200, Type: "expense", TransactionDate: p.Start})
	txRepo.seed(&models.Transaction{ID: 2, AccountID: 1, CategoryID: 9, Amount: 460, Type: "expense", TransactionDate: p.Start.AddDate(0, 0, 1)})

	r := setupBudgetRouter(bRepo, catRepo, accRepo, txRepo, 1, models.RoleClient)
	w := performRequest(r, "GET", "/budgets", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}
	v := decodeBudgetView(t, w)
	if !moneyEq(v.TotalLimit, 8000) {
		t.Fatalf("expected total limit 8000, got %v", v.TotalLimit)
	}
	if !moneyEq(v.TotalSpent, 4660) {
		t.Fatalf("expected total spent 4660, got %v", v.TotalSpent)
	}
	if !moneyEq(v.TotalLeft, 3340) {
		t.Fatalf("expected remaining 3340, got %v", v.TotalLeft)
	}
}

func TestGetBudget_NotFound(t *testing.T) {
	r := setupBudgetRouter(newFakeBudgetRepo(), newFakeCategoryRepo(), newFakeAccountRepo(), newFakeTransactionRepo(), 1, models.RoleClient)
	w := performRequest(r, "GET", "/budgets", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestGetBudget_OnlySumsExpenses(t *testing.T) {
	bRepo := newFakeBudgetRepo()
	catRepo := newFakeCategoryRepo()
	seedExpenseCategories(catRepo, 1)
	accRepo := newFakeAccountRepo()
	accRepo.seed(&models.Account{ID: 1, Name: "Nakit", UserID: 1})
	txRepo := newFakeTransactionRepo()

	b := &models.Budget{ID: 1, UserID: 1, Name: "Aylık", StartDate: models.CivilDate(time.Now().AddDate(0, 0, -5)), PeriodDays: 30}
	bRepo.seed(b, []models.BudgetCategory{{ID: 1, BudgetID: 1, CategoryID: 3, LimitAmount: 6000}})
	p := currentPeriod(b)
	txRepo.seed(&models.Transaction{ID: 1, AccountID: 1, CategoryID: 3, Amount: 500, Type: "expense", TransactionDate: p.Start})
	// an INCOME entry in the same category — must NOT count toward the budget
	txRepo.seed(&models.Transaction{ID: 2, AccountID: 1, CategoryID: 3, Amount: 9999, Type: "income", TransactionDate: p.Start})

	r := setupBudgetRouter(bRepo, catRepo, accRepo, txRepo, 1, models.RoleClient)
	w := performRequest(r, "GET", "/budgets", "")
	v := decodeBudgetView(t, w)
	if !moneyEq(v.TotalSpent, 500) {
		t.Fatalf("only the expense should have counted (500), got %v", v.TotalSpent)
	}
}

// SECURITY: a transaction on another user's account must not count toward
// my budget, even in the same category.
func TestGetBudget_OnlySumsOwnAccounts(t *testing.T) {
	bRepo := newFakeBudgetRepo()
	catRepo := newFakeCategoryRepo()
	seedExpenseCategories(catRepo, 1)
	accRepo := newFakeAccountRepo()
	accRepo.seed(&models.Account{ID: 1, Name: "Benim", UserID: 1})
	accRepo.seed(&models.Account{ID: 2, Name: "Başkası", UserID: 2})
	txRepo := newFakeTransactionRepo()

	b := &models.Budget{ID: 1, UserID: 1, Name: "Aylık", StartDate: models.CivilDate(time.Now().AddDate(0, 0, -5)), PeriodDays: 30}
	bRepo.seed(b, []models.BudgetCategory{{ID: 1, BudgetID: 1, CategoryID: 3, LimitAmount: 6000}})
	p := currentPeriod(b)
	txRepo.seed(&models.Transaction{ID: 1, AccountID: 1, CategoryID: 3, Amount: 300, Type: "expense", TransactionDate: p.Start})
	txRepo.seed(&models.Transaction{ID: 2, AccountID: 2, CategoryID: 3, Amount: 7000, Type: "expense", TransactionDate: p.Start})

	r := setupBudgetRouter(bRepo, catRepo, accRepo, txRepo, 1, models.RoleClient)
	w := performRequest(r, "GET", "/budgets", "")
	v := decodeBudgetView(t, w)
	if !moneyEq(v.TotalSpent, 300) {
		t.Fatalf("only my own account should have counted (300), got %v", v.TotalSpent)
	}
}

func TestGetBudget_IncludesPeriodStartDay(t *testing.T) {
	bRepo := newFakeBudgetRepo()
	catRepo := newFakeCategoryRepo()
	seedExpenseCategories(catRepo, 1)
	accRepo := newFakeAccountRepo()
	accRepo.seed(&models.Account{ID: 1, Name: "Nakit", UserID: 1})
	txRepo := newFakeTransactionRepo()

	b := &models.Budget{ID: 1, UserID: 1, Name: "Aylık", StartDate: models.CivilDate(time.Now().AddDate(0, 0, -5)), PeriodDays: 30}
	bRepo.seed(b, []models.BudgetCategory{{ID: 1, BudgetID: 1, CategoryID: 3, LimitAmount: 6000}})
	p := currentPeriod(b)
	// Exactly at the period start: INCLUDED (>= from)
	txRepo.seed(&models.Transaction{ID: 1, AccountID: 1, CategoryID: 3, Amount: 111, Type: "expense", TransactionDate: p.Start})

	r := setupBudgetRouter(bRepo, catRepo, accRepo, txRepo, 1, models.RoleClient)
	w := performRequest(r, "GET", "/budgets", "")
	v := decodeBudgetView(t, w)
	if !moneyEq(v.TotalSpent, 111) {
		t.Fatalf("the transaction at the period start should have been included, got %v", v.TotalSpent)
	}
}

func TestGetBudget_ExcludesPeriodEndDay(t *testing.T) {
	bRepo := newFakeBudgetRepo()
	catRepo := newFakeCategoryRepo()
	seedExpenseCategories(catRepo, 1)
	accRepo := newFakeAccountRepo()
	accRepo.seed(&models.Account{ID: 1, Name: "Nakit", UserID: 1})
	txRepo := newFakeTransactionRepo()

	b := &models.Budget{ID: 1, UserID: 1, Name: "Aylık", StartDate: models.CivilDate(time.Now().AddDate(0, 0, -5)), PeriodDays: 30}
	bRepo.seed(b, []models.BudgetCategory{{ID: 1, BudgetID: 1, CategoryID: 3, LimitAmount: 6000}})
	p := currentPeriod(b)
	// Exactly at the period end: EXCLUDED (< to) — belongs to the next period
	txRepo.seed(&models.Transaction{ID: 1, AccountID: 1, CategoryID: 3, Amount: 222, Type: "expense", TransactionDate: p.End})

	r := setupBudgetRouter(bRepo, catRepo, accRepo, txRepo, 1, models.RoleClient)
	w := performRequest(r, "GET", "/budgets", "")
	v := decodeBudgetView(t, w)
	if !moneyEq(v.TotalSpent, 0) {
		t.Fatalf("the transaction at the period end should have been excluded (0), got %v", v.TotalSpent)
	}
}

func TestGetBudget_PastPeriodOffset(t *testing.T) {
	bRepo := newFakeBudgetRepo()
	catRepo := newFakeCategoryRepo()
	seedExpenseCategories(catRepo, 1)
	accRepo := newFakeAccountRepo()
	accRepo.seed(&models.Account{ID: 1, Name: "Nakit", UserID: 1})
	txRepo := newFakeTransactionRepo()

	b := &models.Budget{ID: 1, UserID: 1, Name: "Aylık", StartDate: models.CivilDate(time.Now().AddDate(0, 0, -5)), PeriodDays: 30}
	bRepo.seed(b, []models.BudgetCategory{{ID: 1, BudgetID: 1, CategoryID: 3, LimitAmount: 6000}})

	r := setupBudgetRouter(bRepo, catRepo, accRepo, txRepo, 1, models.RoleClient)
	w := performRequest(r, "GET", "/budgets?offset=-1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	v := decodeBudgetView(t, w)
	if !v.Period.Historical {
		t.Fatalf("a past period should have historical:true")
	}
	current := currentPeriod(b)
	if v.Period.StartDate == current.Start.Format(models.DateLayout) {
		t.Fatalf("the past period's start matched the current period: %s", v.Period.StartDate)
	}
}

func TestGetBudget_InvalidOffsetFormat(t *testing.T) {
	bRepo := newFakeBudgetRepo()
	bRepo.seed(&models.Budget{ID: 1, UserID: 1, Name: "X", StartDate: models.CivilDate(time.Now()), PeriodDays: 30}, nil)
	r := setupBudgetRouter(bRepo, newFakeCategoryRepo(), newFakeAccountRepo(), newFakeTransactionRepo(), 1, models.RoleClient)
	w := performRequest(r, "GET", "/budgets?offset=abc", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestGetBudget_OffsetOutOfRange(t *testing.T) {
	bRepo := newFakeBudgetRepo()
	bRepo.seed(&models.Budget{ID: 1, UserID: 1, Name: "X", StartDate: models.CivilDate(time.Now()), PeriodDays: 30}, nil)
	r := setupBudgetRouter(bRepo, newFakeCategoryRepo(), newFakeAccountRepo(), newFakeTransactionRepo(), 1, models.RoleClient)
	w := performRequest(r, "GET", "/budgets?offset=99999", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 (offset out of range), got %d", w.Code)
	}
}

func TestGetBudget_UnspentCategoryReportsZero(t *testing.T) {
	bRepo := newFakeBudgetRepo()
	catRepo := newFakeCategoryRepo()
	seedExpenseCategories(catRepo, 1)
	accRepo := newFakeAccountRepo()
	accRepo.seed(&models.Account{ID: 1, Name: "Nakit", UserID: 1})
	txRepo := newFakeTransactionRepo()

	b := &models.Budget{ID: 1, UserID: 1, Name: "Aylık", StartDate: models.CivilDate(time.Now().AddDate(0, 0, -5)), PeriodDays: 30}
	bRepo.seed(b, []models.BudgetCategory{{ID: 1, BudgetID: 1, CategoryID: 3, LimitAmount: 6000}})

	r := setupBudgetRouter(bRepo, catRepo, accRepo, txRepo, 1, models.RoleClient)
	w := performRequest(r, "GET", "/budgets", "")
	v := decodeBudgetView(t, w)
	if len(v.Categories) != 1 || !moneyEq(v.Categories[0].Spent, 0) {
		t.Fatalf("a category with no spending should report 0, got %+v", v.Categories)
	}
	if !moneyEq(v.Categories[0].Remaining, 6000) {
		t.Fatalf("expected remaining 6000, got %v", v.Categories[0].Remaining)
	}
}

func TestGetBudget_OverLimitReportsNegativeRemaining(t *testing.T) {
	bRepo := newFakeBudgetRepo()
	catRepo := newFakeCategoryRepo()
	seedExpenseCategories(catRepo, 1)
	accRepo := newFakeAccountRepo()
	accRepo.seed(&models.Account{ID: 1, Name: "Nakit", UserID: 1})
	txRepo := newFakeTransactionRepo()

	b := &models.Budget{ID: 1, UserID: 1, Name: "Aylık", StartDate: models.CivilDate(time.Now().AddDate(0, 0, -5)), PeriodDays: 30}
	bRepo.seed(b, []models.BudgetCategory{{ID: 1, BudgetID: 1, CategoryID: 3, LimitAmount: 2000}})
	p := currentPeriod(b)
	txRepo.seed(&models.Transaction{ID: 1, AccountID: 1, CategoryID: 3, Amount: 2350.50, Type: "expense", TransactionDate: p.Start})

	r := setupBudgetRouter(bRepo, catRepo, accRepo, txRepo, 1, models.RoleClient)
	w := performRequest(r, "GET", "/budgets", "")
	v := decodeBudgetView(t, w)
	if !v.Categories[0].OverLimit {
		t.Fatalf("over_limit should have been true")
	}
	if !moneyEq(v.Categories[0].Remaining, -350.50) {
		t.Fatalf("expected remaining -350.50, got %v", v.Categories[0].Remaining)
	}
}

func TestGetBudget_UserWithNoAccountsReportsZeroSpent(t *testing.T) {
	bRepo := newFakeBudgetRepo()
	catRepo := newFakeCategoryRepo()
	seedExpenseCategories(catRepo, 1)
	txRepo := newFakeTransactionRepo()

	b := &models.Budget{ID: 1, UserID: 1, Name: "Aylık", StartDate: models.CivilDate(time.Now().AddDate(0, 0, -5)), PeriodDays: 30}
	bRepo.seed(b, []models.BudgetCategory{{ID: 1, BudgetID: 1, CategoryID: 3, LimitAmount: 6000}})

	r := setupBudgetRouter(bRepo, catRepo, newFakeAccountRepo(), txRepo, 1, models.RoleClient)
	w := performRequest(r, "GET", "/budgets", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	v := decodeBudgetView(t, w)
	if !moneyEq(v.TotalSpent, 0) {
		t.Fatalf("a user with no accounts should show 0 spent, got %v", v.TotalSpent)
	}
}

// --- Update ---

func TestUpdateBudget_ReplacesCategories(t *testing.T) {
	bRepo := newFakeBudgetRepo()
	catRepo := newFakeCategoryRepo()
	seedExpenseCategories(catRepo, 1)
	b := &models.Budget{ID: 1, UserID: 1, Name: "Eski", StartDate: models.CivilDate(time.Now()), PeriodDays: 30}
	bRepo.seed(b, []models.BudgetCategory{{ID: 1, BudgetID: 1, CategoryID: 3, LimitAmount: 6000}})
	r := setupBudgetRouter(bRepo, catRepo, newFakeAccountRepo(), newFakeTransactionRepo(), 1, models.RoleClient)

	body := `{"name":"Yeni","start_date":"2026-02-01","period_days":15,"categories":[{"category_id":9,"limit_amount":1000}]}`
	w := performRequest(r, "PUT", "/budgets", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}
	lines := bRepo.lines[1]
	if len(lines) != 1 || lines[0].CategoryID != 9 {
		t.Fatalf("categories were not replaced, an old line may remain: %+v", lines)
	}
	if bRepo.budgets[1].PeriodDays != 15 || bRepo.budgets[1].Name != "Yeni" {
		t.Fatalf("the header was not updated: %+v", bRepo.budgets[1])
	}
}

func TestUpdateBudget_NotFound(t *testing.T) {
	r := setupBudgetRouter(newFakeBudgetRepo(), newFakeCategoryRepo(), newFakeAccountRepo(), newFakeTransactionRepo(), 1, models.RoleClient)
	body := `{"name":"Yeni","start_date":"2026-02-01","period_days":15,"categories":[{"category_id":9,"limit_amount":1000}]}`
	w := performRequest(r, "PUT", "/budgets", body)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestUpdateBudget_InvalidInput(t *testing.T) {
	bRepo := newFakeBudgetRepo()
	bRepo.seed(&models.Budget{ID: 1, UserID: 1, Name: "Var", StartDate: models.CivilDate(time.Now()), PeriodDays: 30}, nil)
	r := setupBudgetRouter(bRepo, newFakeCategoryRepo(), newFakeAccountRepo(), newFakeTransactionRepo(), 1, models.RoleClient)
	body := `{"name":"Yeni","start_date":"2026-02-01","period_days":0,"categories":[{"category_id":9,"limit_amount":1000}]}`
	w := performRequest(r, "PUT", "/budgets", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// --- Delete ---

func TestDeleteBudget_Success(t *testing.T) {
	bRepo := newFakeBudgetRepo()
	bRepo.seed(&models.Budget{ID: 1, UserID: 1, Name: "Var", StartDate: models.CivilDate(time.Now()), PeriodDays: 30},
		[]models.BudgetCategory{{ID: 1, BudgetID: 1, CategoryID: 3, LimitAmount: 6000}})
	r := setupBudgetRouter(bRepo, newFakeCategoryRepo(), newFakeAccountRepo(), newFakeTransactionRepo(), 1, models.RoleClient)

	w := performRequest(r, "DELETE", "/budgets", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(bRepo.budgets) != 0 {
		t.Fatalf("budget was not deleted")
	}
	if len(bRepo.lines[1]) != 0 {
		t.Fatalf("budget lines were not deleted")
	}
}

func TestDeleteBudget_NotFound(t *testing.T) {
	r := setupBudgetRouter(newFakeBudgetRepo(), newFakeCategoryRepo(), newFakeAccountRepo(), newFakeTransactionRepo(), 1, models.RoleClient)
	w := performRequest(r, "DELETE", "/budgets", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}
