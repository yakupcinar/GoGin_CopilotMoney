package handlers

import (
	"GoGinMoneyCopilot/models"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func setupTransactionRouter(txRepo *fakeTransactionRepo, accRepo *fakeAccountRepo, userID int, role models.Role) *gin.Engine {
	h := NewTransactionHandler(txRepo, accRepo)
	r := gin.New()
	r.Use(authAs(userID, role))
	r.POST("/transactions", h.CreateTransaction)
	r.GET("/transactions/:id", h.GetTransaction)
	r.PUT("/transactions/:id", h.UpdateTransaction)
	r.DELETE("/transactions/:id", h.DeleteTransaction)
	r.GET("/accounts/:id/transactions", h.ListAccountTransactions)
	return r
}

func TestCreateTransaction_Success(t *testing.T) {
	accRepo := newFakeAccountRepo()
	accRepo.seed(&models.Account{ID: 1, Name: "Hesap", UserID: 1})
	txRepo := newFakeTransactionRepo()
	r := setupTransactionRouter(txRepo, accRepo, 1, models.RoleClient)

	body := `{"account_id":1,"category_id":1,"amount":150.50,"type":"income","description":"Maas","transaction_date":"2026-07-13T00:00:00Z"}`
	w := performRequest(r, "POST", "/transactions", body)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (body: %s)", w.Code, w.Body.String())
	}
	if len(txRepo.transactions) != 1 {
		t.Fatalf("transaction was not created")
	}
	if txRepo.transactions[1].Amount != 150.50 {
		t.Fatalf("amount was stored incorrectly: %v", txRepo.transactions[1].Amount)
	}
}

// SECURITY: a user cannot add a transaction to an account they do not own.
func TestCreateTransaction_ForeignAccountRejected(t *testing.T) {
	accRepo := newFakeAccountRepo()
	accRepo.seed(&models.Account{ID: 1, Name: "User1 Hesap", UserID: 1})
	txRepo := newFakeTransactionRepo()

	// user 2 attempts to add a transaction to user 1's account
	r := setupTransactionRouter(txRepo, accRepo, 2, models.RoleClient)
	body := `{"account_id":1,"category_id":1,"amount":100,"type":"expense","transaction_date":"2026-07-13T00:00:00Z"}`
	w := performRequest(r, "POST", "/transactions", body)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if len(txRepo.transactions) != 0 {
		t.Fatalf("a transaction was added to another user's account")
	}
}

func TestCreateTransaction_NegativeAmountRejected(t *testing.T) {
	accRepo := newFakeAccountRepo()
	accRepo.seed(&models.Account{ID: 1, Name: "Hesap", UserID: 1})
	txRepo := newFakeTransactionRepo()
	r := setupTransactionRouter(txRepo, accRepo, 1, models.RoleClient)

	// the amount binding is "gt=0" -> a negative value must yield 400
	body := `{"account_id":1,"category_id":1,"amount":-50,"type":"income","transaction_date":"2026-07-13T00:00:00Z"}`
	w := performRequest(r, "POST", "/transactions", body)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestGetTransaction_OwnerCanRead(t *testing.T) {
	accRepo := newFakeAccountRepo()
	accRepo.seed(&models.Account{ID: 1, Name: "Hesap", UserID: 1})
	txRepo := newFakeTransactionRepo()
	txRepo.seed(&models.Transaction{ID: 1, AccountID: 1, CategoryID: 1, Amount: 10, Type: "income", TransactionDate: time.Now()})

	r := setupTransactionRouter(txRepo, accRepo, 1, models.RoleClient)
	w := performRequest(r, "GET", "/transactions/1", "")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// SECURITY: a transaction cannot be read if it belongs to another user's account.
func TestGetTransaction_OwnershipIsolation(t *testing.T) {
	accRepo := newFakeAccountRepo()
	accRepo.seed(&models.Account{ID: 1, Name: "User1 Hesap", UserID: 1})
	txRepo := newFakeTransactionRepo()
	txRepo.seed(&models.Transaction{ID: 1, AccountID: 1, CategoryID: 1, Amount: 10, Type: "income", TransactionDate: time.Now()})

	r := setupTransactionRouter(txRepo, accRepo, 2, models.RoleClient)
	w := performRequest(r, "GET", "/transactions/1", "")

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestListAccountTransactions_OnlyThatAccount(t *testing.T) {
	accRepo := newFakeAccountRepo()
	accRepo.seed(&models.Account{ID: 1, Name: "Hesap1", UserID: 1})
	accRepo.seed(&models.Account{ID: 2, Name: "Hesap2", UserID: 1})
	txRepo := newFakeTransactionRepo()
	txRepo.seed(&models.Transaction{ID: 1, AccountID: 1, Amount: 10, Type: "income", TransactionDate: time.Now()})
	txRepo.seed(&models.Transaction{ID: 2, AccountID: 1, Amount: 20, Type: "expense", TransactionDate: time.Now()})
	txRepo.seed(&models.Transaction{ID: 3, AccountID: 2, Amount: 30, Type: "income", TransactionDate: time.Now()})

	r := setupTransactionRouter(txRepo, accRepo, 1, models.RoleClient)
	w := performRequest(r, "GET", "/accounts/1/transactions", "")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var got struct {
		Transactions []models.Transaction `json:"transactions"`
		Total        int64                `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("response could not be parsed: %v", err)
	}
	if len(got.Transactions) != 2 {
		t.Fatalf("expected 2 transactions for account 1, got %d", len(got.Transactions))
	}
	if got.Total != 2 {
		t.Fatalf("expected total 2, got %d", got.Total)
	}
}

// Pagination: with page_size=1 only 1 record must be returned, but total must
// still reflect the real count — the client compares the two to know whether
// more pages exist.
func TestListAccountTransactions_Pagination(t *testing.T) {
	accRepo := newFakeAccountRepo()
	accRepo.seed(&models.Account{ID: 1, Name: "Hesap1", UserID: 1})
	txRepo := newFakeTransactionRepo()
	txRepo.seed(&models.Transaction{ID: 1, AccountID: 1, Amount: 10, Type: "income", TransactionDate: time.Now()})
	txRepo.seed(&models.Transaction{ID: 2, AccountID: 1, Amount: 20, Type: "expense", TransactionDate: time.Now()})

	r := setupTransactionRouter(txRepo, accRepo, 1, models.RoleClient)
	w := performRequest(r, "GET", "/accounts/1/transactions?page=1&page_size=1", "")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var got struct {
		Transactions []models.Transaction `json:"transactions"`
		Total        int64                `json:"total"`
		PageSize     int                  `json:"page_size"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("response could not be parsed: %v", err)
	}
	if len(got.Transactions) != 1 {
		t.Fatalf("expected 1 transaction with page_size=1, got %d", len(got.Transactions))
	}
	if got.Total != 2 {
		t.Fatalf("total should reflect the real count (2), got %d", got.Total)
	}
}

func TestListAccountTransactions_OwnershipIsolation(t *testing.T) {
	accRepo := newFakeAccountRepo()
	accRepo.seed(&models.Account{ID: 1, Name: "User1 Hesap", UserID: 1})
	txRepo := newFakeTransactionRepo()
	txRepo.seed(&models.Transaction{ID: 1, AccountID: 1, Amount: 10, Type: "income", TransactionDate: time.Now()})

	r := setupTransactionRouter(txRepo, accRepo, 2, models.RoleClient)
	w := performRequest(r, "GET", "/accounts/1/transactions", "")

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestUpdateTransaction_Success(t *testing.T) {
	accRepo := newFakeAccountRepo()
	accRepo.seed(&models.Account{ID: 1, Name: "Hesap", UserID: 1})
	txRepo := newFakeTransactionRepo()
	txRepo.seed(&models.Transaction{ID: 1, AccountID: 1, CategoryID: 1, Amount: 10, Type: "income", TransactionDate: time.Now()})

	r := setupTransactionRouter(txRepo, accRepo, 1, models.RoleClient)
	body := `{"category_id":2,"amount":99.9,"type":"expense","description":"Guncel","transaction_date":"2026-07-14T00:00:00Z"}`
	w := performRequest(r, "PUT", "/transactions/1", body)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}
	if txRepo.transactions[1].Amount != 99.9 || txRepo.transactions[1].Type != "expense" {
		t.Fatalf("transaction was not updated: %+v", txRepo.transactions[1])
	}
}

func TestUpdateTransaction_OwnershipIsolation(t *testing.T) {
	accRepo := newFakeAccountRepo()
	accRepo.seed(&models.Account{ID: 1, Name: "User1 Hesap", UserID: 1})
	txRepo := newFakeTransactionRepo()
	txRepo.seed(&models.Transaction{ID: 1, AccountID: 1, CategoryID: 1, Amount: 10, Type: "income", TransactionDate: time.Now()})

	r := setupTransactionRouter(txRepo, accRepo, 2, models.RoleClient)
	body := `{"category_id":2,"amount":99.9,"type":"expense","transaction_date":"2026-07-14T00:00:00Z"}`
	w := performRequest(r, "PUT", "/transactions/1", body)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if txRepo.transactions[1].Amount != 10 {
		t.Fatalf("another user modified the transaction")
	}
}

func TestDeleteTransaction_Success(t *testing.T) {
	accRepo := newFakeAccountRepo()
	accRepo.seed(&models.Account{ID: 1, Name: "Hesap", UserID: 1})
	txRepo := newFakeTransactionRepo()
	txRepo.seed(&models.Transaction{ID: 1, AccountID: 1, Amount: 10, Type: "income", TransactionDate: time.Now()})

	r := setupTransactionRouter(txRepo, accRepo, 1, models.RoleClient)
	w := performRequest(r, "DELETE", "/transactions/1", "")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(txRepo.transactions) != 0 {
		t.Fatalf("transaction was not deleted")
	}
}

func TestDeleteTransaction_OwnershipIsolation(t *testing.T) {
	accRepo := newFakeAccountRepo()
	accRepo.seed(&models.Account{ID: 1, Name: "User1 Hesap", UserID: 1})
	txRepo := newFakeTransactionRepo()
	txRepo.seed(&models.Transaction{ID: 1, AccountID: 1, Amount: 10, Type: "income", TransactionDate: time.Now()})

	r := setupTransactionRouter(txRepo, accRepo, 2, models.RoleClient)
	w := performRequest(r, "DELETE", "/transactions/1", "")

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if len(txRepo.transactions) != 1 {
		t.Fatalf("another user deleted the transaction")
	}
}

// An admin must be able to read another user's transaction as well.
func TestGetTransaction_AdminCanReadAny(t *testing.T) {
	accRepo := newFakeAccountRepo()
	accRepo.seed(&models.Account{ID: 1, Name: "User1 Hesap", UserID: 1})
	txRepo := newFakeTransactionRepo()
	txRepo.seed(&models.Transaction{ID: 1, AccountID: 1, Amount: 10, Type: "income", TransactionDate: time.Now()})

	r := setupTransactionRouter(txRepo, accRepo, 99, models.RoleAdmin)
	w := performRequest(r, "GET", "/transactions/1", "")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin, got %d", w.Code)
	}
}
