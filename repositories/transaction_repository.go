package repositories

import (
	"GoGinMoneyCopilot/models"
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

var ErrTransactionNotFound = errors.New("Transaction Not Found!")

type TransactionRepository interface {
	Create(ctx context.Context, input models.CreateTransactionInput) error
	GetByID(ctx context.Context, transactionID int) (*models.Transaction, error)
	ListByAccount(ctx context.Context, accountID int) ([]models.Transaction, error)
	CountByCategory(ctx context.Context, categoryID int) (int64, error)
	SumExpenseByCategory(ctx context.Context, accountIDs []int, from, to time.Time) (map[int]float64, error)
	Update(ctx context.Context, transactionID int, input models.UpdateTransactionInput) error
	Delete(ctx context.Context, transactionID int) error
}

type gormTransactionRepository struct {
	db *gorm.DB
}

func NewTransactionRepository(db *gorm.DB) TransactionRepository {
	return &gormTransactionRepository{db: db}
}

func (r *gormTransactionRepository) Create(ctx context.Context, input models.CreateTransactionInput) error {
	tx := models.Transaction{
		AccountID:       input.AccountID,
		CategoryID:      input.CategoryID,
		Amount:          input.Amount,
		Type:            input.Type,
		Description:     input.Description,
		TransactionDate: input.TransactionDate,
	}
	if err := r.db.WithContext(ctx).Create(&tx).Error; err != nil {
		return fmt.Errorf("Transaction couldn't be created: %v", err)
	}
	return nil
}

func (r *gormTransactionRepository) GetByID(ctx context.Context, transactionID int) (*models.Transaction, error) {
	var tx models.Transaction
	if err := r.db.WithContext(ctx).First(&tx, transactionID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTransactionNotFound
		}
		return nil, fmt.Errorf("Transaction Couldn't Be Fetched: %v", err)
	}
	return &tx, nil
}

func (r *gormTransactionRepository) ListByAccount(ctx context.Context, accountID int) ([]models.Transaction, error) {
	var transactions []models.Transaction
	if err := r.db.WithContext(ctx).Where("account_id = ?", accountID).Order("transaction_date DESC").Find(&transactions).Error; err != nil {
		return nil, fmt.Errorf("Transactions couldn't be fetched: %v", err)
	}
	return transactions, nil
}

func (r *gormTransactionRepository) CountByCategory(ctx context.Context, categoryID int) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&models.Transaction{}).
		Where("category_id = ?", categoryID).Count(&count).Error; err != nil {
		return 0, fmt.Errorf("Transaction count couldn't be fetched: %v", err)
	}
	return count, nil
}

// SumExpenseByCategory — verilen hesaplardaki GİDER işlemlerinin [from, to)
// tarih aralığında kategori bazlı toplamı.
//
// NEDEN JOIN YOK: bu projede sahiplik ya WHERE user_id = ? ile ya da bir ID
// listesiyle taşınır. Hesap listesini zaten AccountRepository.ListForUser
// üretiyor; JOIN eklemek sahiplik mantığını ikinci bir yere dağıtırdı.
//
// NEDEN created_at DEĞİL transaction_date: kullanıcı dün harcadığı parayı
// bugün girebilir. Bütçeyi ilgilendiren, paranın harcandığı gündür.
//
// ARALIK YARI AÇIK [from, to): kapalı olsaydı bir dönemin son günü ile bir
// sonraki dönemin ilk günü aynı gün olur ve o günün harcaması İKİ kez sayılırdı.
//
// DÖNEN HARİTA: harcaması olmayan kategori haritada HİÇ BULUNMAZ. Çağıran,
// eksik anahtarı Go'nun sıfır değeriyle 0 olarak okur.
func (r *gormTransactionRepository) SumExpenseByCategory(ctx context.Context, accountIDs []int, from, to time.Time) (map[int]float64, error) {
	sums := map[int]float64{}
	// Hiç hesabı olmayan kullanıcı: sorguyu hiç açma.
	if len(accountIDs) == 0 {
		return sums, nil
	}

	var rows []struct {
		CategoryID int
		Total      float64
	}
	if err := r.db.WithContext(ctx).Model(&models.Transaction{}).
		Select("category_id, COALESCE(SUM(amount), 0) AS total").
		Where("account_id IN ? AND type = ? AND transaction_date >= ? AND transaction_date < ?",
			accountIDs, "expense", from, to).
		Group("category_id").
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("Category spending couldn't be fetched: %v", err)
	}

	for _, row := range rows {
		sums[row.CategoryID] = row.Total
	}
	return sums, nil
}

func (r *gormTransactionRepository) Update(ctx context.Context, transactionID int, input models.UpdateTransactionInput) error {
	result := r.db.WithContext(ctx).Model(&models.Transaction{}).Where("id = ?", transactionID).Updates(map[string]interface{}{
		"category_id":      input.CategoryID,
		"amount":           input.Amount,
		"type":             input.Type,
		"description":      input.Description,
		"transaction_date": input.TransactionDate,
	})
	if result.Error != nil {
		return fmt.Errorf("Transaction couldn't be updated: %v", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrTransactionNotFound
	}
	return nil
}

func (r *gormTransactionRepository) Delete(ctx context.Context, transactionID int) error {
	result := r.db.WithContext(ctx).Delete(&models.Transaction{}, transactionID)
	if result.Error != nil {
		return fmt.Errorf("Transaction can't deleted: %v", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrTransactionNotFound
	}
	return nil
}
