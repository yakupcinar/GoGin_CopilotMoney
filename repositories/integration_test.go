//go:build integration

// Gerçek Postgres'e karşı entegrasyon testleri.
//
// NEDEN AYRI (build tag): normal `go test` bunları ÇALIŞTIRMAZ — Postgres
// gerektirir ve yavaştır. Çalıştırmak için: go test -tags integration ./repositories/
//
// NEDEN GEREKLİ: birim testleri sahte repo kullanır; sahteler gerçek SQL'i
// Go'da YENİDEN YAZAR (ör. SumExpenseByCategory'nin GROUP BY'ı bir for
// döngüsü olur). Bu dosya SADECE gerçek veritabanında var olan şeyleri
// doğrular: gerçek SQL, GORM'un ürettiği FK'lar, unique indeksler, CHECK.
//
// GÜVENLİK: ayrı bir veritabanı (copilot_money_test) kullanır; gerçek/
// geliştirme verisine (copilot_money) HİÇ dokunmaz.
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
		log.Println("DB_* env yok — entegrasyon testleri atlanıyor")
		os.Exit(0)
	}

	// 1) Sunucunun 'postgres' veritabanına bağlan, test DB'sini oluştur.
	adminDSN := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=postgres sslmode=disable",
		host, port, user, pass)
	admin, err := gorm.Open(postgres.Open(adminDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		log.Fatalf("admin bağlantısı başarısız: %v", err)
	}
	// Zaten varsa hata verir — yok say.
	admin.Exec("CREATE DATABASE " + testDBName)

	// 2) Test veritabanına bağlan + migrate.
	testDSN := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		host, port, user, pass, testDBName)
	testDB, err = gorm.Open(postgres.Open(testDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		log.Fatalf("test DB bağlantısı başarısız: %v", err)
	}
	if err := testDB.AutoMigrate(
		&models.User{}, &models.Account{}, &models.Category{}, &models.Transaction{},
		&models.Budget{}, &models.BudgetCategory{},
		&models.RevokedToken{}, &models.PendingAction{}, &models.RefreshToken{},
	); err != nil {
		log.Fatalf("migrate başarısız: %v", err)
	}

	os.Exit(m.Run())
}

// truncate — her testten önce ilgili tabloları temizler (izolasyon).
func truncate(t *testing.T) {
	t.Helper()
	if err := testDB.Exec(
		"TRUNCATE budget_categories, budgets, transactions, categories, accounts RESTART IDENTITY CASCADE").Error; err != nil {
		t.Fatalf("truncate başarısız: %v", err)
	}
}

func moneyClose(a, b float64) bool { return math.Abs(a-b) < 0.001 }

// GERÇEK SQL: GROUP BY + type filtresi + yarı-açık [from, to) aralığı.
// Sahtenin taklit ettiği her kuralı gerçek Postgres'te doğrular.
func TestIntegration_SumExpenseByCategory(t *testing.T) {
	truncate(t)
	repo := NewTransactionRepository(testDB)

	from := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC)
	d := func(day int) time.Time { return time.Date(2026, 7, day, 0, 0, 0, 0, time.UTC) }

	seed := []models.Transaction{
		{AccountID: 1, CategoryID: 1, Amount: 100, Type: "expense", TransactionDate: from},                    // dahil (from sınırı)
		{AccountID: 1, CategoryID: 1, Amount: 50, Type: "expense", TransactionDate: d(15)},                    // dahil
		{AccountID: 1, CategoryID: 1, Amount: 999, Type: "income", TransactionDate: d(15)},                    // HARİÇ (gelir)
		{AccountID: 1, CategoryID: 1, Amount: 200, Type: "expense", TransactionDate: to},                      // HARİÇ (to sınırı, yarı-açık)
		{AccountID: 1, CategoryID: 1, Amount: 300, Type: "expense", TransactionDate: d(30).AddDate(0, -1, 0)}, // HARİÇ (aralık öncesi)
		{AccountID: 1, CategoryID: 2, Amount: 70, Type: "expense", TransactionDate: d(10)},                    // farklı kategori
		{AccountID: 99, CategoryID: 1, Amount: 500, Type: "expense", TransactionDate: d(12)},                  // HARİÇ (başka hesap)
	}
	for i := range seed {
		if err := testDB.Create(&seed[i]).Error; err != nil {
			t.Fatalf("seed başarısız: %v", err)
		}
	}

	sums, err := repo.SumExpenseByCategory([]int{1}, from, to)
	if err != nil {
		t.Fatalf("SumExpenseByCategory hata: %v", err)
	}
	if !moneyClose(sums[1], 150) { // 100 + 50 (gelir/to-sınırı/aralık-öncesi/başka-hesap hariç)
		t.Fatalf("kategori 1 toplamı 150 beklendi, gelen %v", sums[1])
	}
	if !moneyClose(sums[2], 70) {
		t.Fatalf("kategori 2 toplamı 70 beklendi, gelen %v", sums[2])
	}
}

// Unique indeks: kullanıcı başına tek bütçe -> ikinci Create 23505 -> ErrBudgetExists.
func TestIntegration_OneBudgetPerUser(t *testing.T) {
	truncate(t)
	// Kategori 1'i oluştur (bütçe ona referans verecek — FK için gerekli).
	if err := testDB.Create(&models.Category{ID: 1, Name: "Market", Type: "expense"}).Error; err != nil {
		t.Fatalf("kategori seed: %v", err)
	}
	repo := NewBudgetRepository(testDB)
	input := models.CreateBudgetInput{
		Name: "Aylık", PeriodDays: 30,
		Categories: []models.BudgetCategoryInput{{CategoryID: 1, LimitAmount: 1000}},
	}
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	if err := repo.Create(7, input, start); err != nil {
		t.Fatalf("ilk bütçe oluşmalıydı: %v", err)
	}
	err := repo.Create(7, input, start) // aynı kullanıcı, ikinci bütçe
	if !errors.Is(err, ErrBudgetExists) {
		t.Fatalf("ikinci bütçe ErrBudgetExists vermeliydi, gelen: %v", err)
	}
}

// GORM'un ürettiği FK RESTRICT: bütçede kullanılan kategori DB seviyesinde
// silinemez. (Sahte repo bunu asla yakalayamaz — FK gerçek DB'de yaşar.)
func TestIntegration_CategoryUsedByBudget_FKRestrict(t *testing.T) {
	truncate(t)
	if err := testDB.Create(&models.Category{ID: 1, Name: "Market", Type: "expense"}).Error; err != nil {
		t.Fatalf("kategori seed: %v", err)
	}
	budgets := NewBudgetRepository(testDB)
	if err := budgets.Create(7, models.CreateBudgetInput{
		Name: "Aylık", PeriodDays: 30,
		Categories: []models.BudgetCategoryInput{{CategoryID: 1, LimitAmount: 1000}},
	}, time.Now()); err != nil {
		t.Fatalf("bütçe oluşmalıydı: %v", err)
	}

	// category_repository FK ihlalini 23503 -> ErrCategoryInUse'a çevirir.
	err := NewCategoryRepository(testDB).Delete(1)
	if !errors.Is(err, ErrCategoryInUse) {
		t.Fatalf("bütçede kullanılan kategori silinememeli (ErrCategoryInUse), gelen: %v", err)
	}
}

// CHECK kısıtı: period_days 1..365 dışı -> DB reddeder (23514).
func TestIntegration_PeriodDaysCheckConstraint(t *testing.T) {
	truncate(t)
	err := testDB.Create(&models.Budget{
		UserID: 7, Name: "Bozuk", StartDate: time.Now(), PeriodDays: 400,
	}).Error
	if err == nil {
		t.Fatalf("period_days=400 CHECK tarafından reddedilmeliydi")
	}
}
