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
		t.Fatalf("beklenen 201, gelen %d (body: %s)", w.Code, w.Body.String())
	}
	if len(txRepo.transactions) != 1 {
		t.Fatalf("transaction oluşmadı")
	}
	if txRepo.transactions[1].Amount != 150.50 {
		t.Fatalf("tutar yanlış kaydedildi: %v", txRepo.transactions[1].Amount)
	}
}

// GÜVENLİK: kullanıcı, sahibi olmadığı bir hesaba transaction ekleyemez.
func TestCreateTransaction_ForeignAccountRejected(t *testing.T) {
	accRepo := newFakeAccountRepo()
	accRepo.seed(&models.Account{ID: 1, Name: "User1 Hesap", UserID: 1})
	txRepo := newFakeTransactionRepo()

	// user 2, user 1'in hesabına işlem eklemeye çalışıyor
	r := setupTransactionRouter(txRepo, accRepo, 2, models.RoleClient)
	body := `{"account_id":1,"category_id":1,"amount":100,"type":"expense","transaction_date":"2026-07-13T00:00:00Z"}`
	w := performRequest(r, "POST", "/transactions", body)

	if w.Code != http.StatusNotFound {
		t.Fatalf("beklenen 404, gelen %d", w.Code)
	}
	if len(txRepo.transactions) != 0 {
		t.Fatalf("başka kullanıcının hesabına transaction eklendi")
	}
}

func TestCreateTransaction_NegativeAmountRejected(t *testing.T) {
	accRepo := newFakeAccountRepo()
	accRepo.seed(&models.Account{ID: 1, Name: "Hesap", UserID: 1})
	txRepo := newFakeTransactionRepo()
	r := setupTransactionRouter(txRepo, accRepo, 1, models.RoleClient)

	// amount binding'i "gt=0" -> negatif değer 400 vermeli
	body := `{"account_id":1,"category_id":1,"amount":-50,"type":"income","transaction_date":"2026-07-13T00:00:00Z"}`
	w := performRequest(r, "POST", "/transactions", body)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("beklenen 400, gelen %d", w.Code)
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
		t.Fatalf("beklenen 200, gelen %d", w.Code)
	}
}

// GÜVENLİK: transaction başka kullanıcının hesabına aitse okunamaz.
func TestGetTransaction_OwnershipIsolation(t *testing.T) {
	accRepo := newFakeAccountRepo()
	accRepo.seed(&models.Account{ID: 1, Name: "User1 Hesap", UserID: 1})
	txRepo := newFakeTransactionRepo()
	txRepo.seed(&models.Transaction{ID: 1, AccountID: 1, CategoryID: 1, Amount: 10, Type: "income", TransactionDate: time.Now()})

	r := setupTransactionRouter(txRepo, accRepo, 2, models.RoleClient)
	w := performRequest(r, "GET", "/transactions/1", "")

	if w.Code != http.StatusNotFound {
		t.Fatalf("beklenen 404, gelen %d", w.Code)
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
		t.Fatalf("beklenen 200, gelen %d", w.Code)
	}
	var got []models.Transaction
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("cevap parse edilemedi: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("hesap 1 için 2 işlem bekleniyordu, gelen %d", len(got))
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
		t.Fatalf("beklenen 404, gelen %d", w.Code)
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
		t.Fatalf("beklenen 200, gelen %d (body: %s)", w.Code, w.Body.String())
	}
	if txRepo.transactions[1].Amount != 99.9 || txRepo.transactions[1].Type != "expense" {
		t.Fatalf("transaction güncellenmedi: %+v", txRepo.transactions[1])
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
		t.Fatalf("beklenen 404, gelen %d", w.Code)
	}
	if txRepo.transactions[1].Amount != 10 {
		t.Fatalf("başka kullanıcı transaction'ı değiştirdi")
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
		t.Fatalf("beklenen 200, gelen %d", w.Code)
	}
	if len(txRepo.transactions) != 0 {
		t.Fatalf("transaction silinmedi")
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
		t.Fatalf("beklenen 404, gelen %d", w.Code)
	}
	if len(txRepo.transactions) != 1 {
		t.Fatalf("başka kullanıcı transaction'ı sildi")
	}
}

// Admin başka kullanıcının işlemini de görebilmeli.
func TestGetTransaction_AdminCanReadAny(t *testing.T) {
	accRepo := newFakeAccountRepo()
	accRepo.seed(&models.Account{ID: 1, Name: "User1 Hesap", UserID: 1})
	txRepo := newFakeTransactionRepo()
	txRepo.seed(&models.Transaction{ID: 1, AccountID: 1, Amount: 10, Type: "income", TransactionDate: time.Now()})

	r := setupTransactionRouter(txRepo, accRepo, 99, models.RoleAdmin)
	w := performRequest(r, "GET", "/transactions/1", "")

	if w.Code != http.StatusOK {
		t.Fatalf("admin için 200 bekleniyordu, gelen %d", w.Code)
	}
}
