//go:build integration

// Integration tests against a real Postgres.
//
// WHY SEPARATE (build tag): a normal `go test` does NOT run these — they
// need Postgres and are slow. To run: go test -tags integration ./repositories/
//
// WHY NEEDED: unit tests use a fake repo; the fakes REWRITE real SQL in Go
// (e.g. SumExpenseByCategory's GROUP BY becomes a for loop). This file ONLY
// verifies things that exist in the real database: real SQL, GORM-generated
// FKs, unique indexes, CHECK constraints.
//
// SAFETY: uses a separate database (copilot_money_test); it NEVER touches
// the real/development data (copilot_money).
package repositories

import (
	"GoGinMoneyCopilot/models"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDB *gorm.DB

const testDBName = "copilot_money_test"

func TestMain(m *testing.M) {
	_ = godotenv.Load("../.env") // testler paket dizininde çalışır; .env bir üstte

	host := os.Getenv("DB_HOST")
	port := os.Getenv("DB_PORT")
	user := os.Getenv("DB_USER")
	pass := os.Getenv("DB_PASSWORD")
	if host == "" || port == "" || user == "" {
		log.Println("DB_* env not set — skipping integration tests")
		os.Exit(0)
	}

	// 1) Connect to the server's 'postgres' database, create the test DB.
	adminDSN := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=postgres sslmode=disable",
		host, port, user, pass)
	admin, err := gorm.Open(postgres.Open(adminDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		log.Fatalf("admin connection failed: %v", err)
	}
	// Errors if it already exists — ignore.
	admin.Exec("CREATE DATABASE " + testDBName)

	// 2) Connect to the test database + migrate.
	testDSN := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		host, port, user, pass, testDBName)
	testDB, err = gorm.Open(postgres.Open(testDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		log.Fatalf("test DB connection failed: %v", err)
	}
	if err := testDB.AutoMigrate(
		&models.User{}, &models.Account{}, &models.Category{}, &models.Transaction{},
		&models.Budget{}, &models.BudgetCategory{},
		&models.RevokedToken{}, &models.PendingAction{}, &models.RefreshToken{},
	); err != nil {
		log.Fatalf("migrate failed: %v", err)
	}

	os.Exit(m.Run())
}

// truncate — clears the relevant tables before each test (isolation).
func truncate(t *testing.T) {
	t.Helper()
	if err := testDB.Exec(
		"TRUNCATE budget_categories, budgets, transactions, categories, accounts RESTART IDENTITY CASCADE").Error; err != nil {
		t.Fatalf("truncate failed: %v", err)
	}
}

func moneyClose(a, b float64) bool { return math.Abs(a-b) < 0.001 }

// REAL SQL: GROUP BY + type filter + half-open [from, to) range.
// Verifies every rule the fake imitates against real Postgres.
func TestIntegration_SumExpenseByCategory(t *testing.T) {
	truncate(t)
	repo := NewTransactionRepository(testDB)

	from := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC)
	d := func(day int) time.Time { return time.Date(2026, 7, day, 0, 0, 0, 0, time.UTC) }

	seed := []models.Transaction{
		{AccountID: 1, CategoryID: 1, Amount: 100, Type: "expense", TransactionDate: from},                    // included (from boundary)
		{AccountID: 1, CategoryID: 1, Amount: 50, Type: "expense", TransactionDate: d(15)},                    // included
		{AccountID: 1, CategoryID: 1, Amount: 999, Type: "income", TransactionDate: d(15)},                    // EXCLUDED (income)
		{AccountID: 1, CategoryID: 1, Amount: 200, Type: "expense", TransactionDate: to},                      // EXCLUDED (to boundary, half-open)
		{AccountID: 1, CategoryID: 1, Amount: 300, Type: "expense", TransactionDate: d(30).AddDate(0, -1, 0)}, // EXCLUDED (before the range)
		{AccountID: 1, CategoryID: 2, Amount: 70, Type: "expense", TransactionDate: d(10)},                    // different category
		{AccountID: 99, CategoryID: 1, Amount: 500, Type: "expense", TransactionDate: d(12)},                  // EXCLUDED (different account)
	}
	for i := range seed {
		if err := testDB.Create(&seed[i]).Error; err != nil {
			t.Fatalf("seed failed: %v", err)
		}
	}

	sums, err := repo.SumExpenseByCategory([]int{1}, from, to)
	if err != nil {
		t.Fatalf("SumExpenseByCategory error: %v", err)
	}
	if !moneyClose(sums[1], 150) { // 100 + 50 (excludes income/to-boundary/before-range/different-account)
		t.Fatalf("expected category 1 total of 150, got %v", sums[1])
	}
	if !moneyClose(sums[2], 70) {
		t.Fatalf("expected category 2 total of 70, got %v", sums[2])
	}
}

// Unique index: one budget per user -> second Create gets 23505 -> ErrBudgetExists.
func TestIntegration_OneBudgetPerUser(t *testing.T) {
	truncate(t)
	// Create category 1 (the budget will reference it — needed for the FK).
	if err := testDB.Create(&models.Category{ID: 1, Name: "Market", Type: "expense"}).Error; err != nil {
		t.Fatalf("category seed: %v", err)
	}
	repo := NewBudgetRepository(testDB)
	input := models.CreateBudgetInput{
		Name: "Aylık", PeriodDays: 30,
		Categories: []models.BudgetCategoryInput{{CategoryID: 1, LimitAmount: 1000}},
	}
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	if err := repo.Create(7, input, start); err != nil {
		t.Fatalf("the first budget should have been created: %v", err)
	}
	err := repo.Create(7, input, start) // same user, second budget
	if !errors.Is(err, ErrBudgetExists) {
		t.Fatalf("the second budget should have returned ErrBudgetExists, got: %v", err)
	}
}

// GORM-generated FK RESTRICT: a category used by a budget cannot be
// deleted at the DB level. (The fake repo can never catch this — the FK
// lives in the real DB.)
func TestIntegration_CategoryUsedByBudget_FKRestrict(t *testing.T) {
	truncate(t)
	if err := testDB.Create(&models.Category{ID: 1, Name: "Market", Type: "expense"}).Error; err != nil {
		t.Fatalf("category seed: %v", err)
	}
	budgets := NewBudgetRepository(testDB)
	if err := budgets.Create(7, models.CreateBudgetInput{
		Name: "Aylık", PeriodDays: 30,
		Categories: []models.BudgetCategoryInput{{CategoryID: 1, LimitAmount: 1000}},
	}, time.Now()); err != nil {
		t.Fatalf("the budget should have been created: %v", err)
	}

	// category_repository translates the 23503 FK violation -> ErrCategoryInUse.
	err := NewCategoryRepository(testDB).Delete(1)
	if !errors.Is(err, ErrCategoryInUse) {
		t.Fatalf("a category used by a budget must not be deletable (ErrCategoryInUse), got: %v", err)
	}
}

// CHECK constraint: period_days outside 1..365 -> DB rejects it (23514).
func TestIntegration_PeriodDaysCheckConstraint(t *testing.T) {
	truncate(t)
	err := testDB.Create(&models.Budget{
		UserID: 7, Name: "Bozuk", StartDate: time.Now(), PeriodDays: 400,
	}).Error
	if err == nil {
		t.Fatalf("period_days=400 should have been rejected by the CHECK constraint")
	}
}
