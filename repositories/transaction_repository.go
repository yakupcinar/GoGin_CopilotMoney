package repositories

import (
	"GoGinMoneyCopilot/models"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

var ErrTransactionNotFound = errors.New("Transaction Not Found!")

type TransactionRepository interface {
	Create(input models.CreateTransactionInput) error
	GetByID(transactionID int) (*models.Transaction, error)
	ListByAccount(accountID int) ([]models.Transaction, error)
	CountByCategory(categoryID int) (int64, error)
	Update(transactionID int, input models.UpdateTransactionInput) error
	Delete(transactionID int) error
}

type gormTransactionRepository struct {
	db *gorm.DB
}

func NewTransactionRepository(db *gorm.DB) TransactionRepository {
	return &gormTransactionRepository{db: db}
}

func (r *gormTransactionRepository) Create(input models.CreateTransactionInput) error {
	tx := models.Transaction{
		AccountID:       input.AccountID,
		CategoryID:      input.CategoryID,
		Amount:          input.Amount,
		Type:            input.Type,
		Description:     input.Description,
		TransactionDate: input.TransactionDate,
	}
	if err := r.db.Create(&tx).Error; err != nil {
		return fmt.Errorf("Transaction couldn't be created: %v", err)
	}
	return nil
}

func (r *gormTransactionRepository) GetByID(transactionID int) (*models.Transaction, error) {
	var tx models.Transaction
	if err := r.db.First(&tx, transactionID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTransactionNotFound
		}
		return nil, fmt.Errorf("Transaction Couldn't Be Fetched: %v", err)
	}
	return &tx, nil
}

func (r *gormTransactionRepository) ListByAccount(accountID int) ([]models.Transaction, error) {
	var transactions []models.Transaction
	if err := r.db.Where("account_id = ?", accountID).Order("transaction_date DESC").Find(&transactions).Error; err != nil {
		return nil, fmt.Errorf("Transactions couldn't be fetched: %v", err)
	}
	return transactions, nil
}

func (r *gormTransactionRepository) CountByCategory(categoryID int) (int64, error) {
	var count int64
	if err := r.db.Model(&models.Transaction{}).
		Where("category_id = ?", categoryID).Count(&count).Error; err != nil {
		return 0, fmt.Errorf("Transaction count couldn't be fetched: %v", err)
	}
	return count, nil
}

func (r *gormTransactionRepository) Update(transactionID int, input models.UpdateTransactionInput) error {
	result := r.db.Model(&models.Transaction{}).Where("id = ?", transactionID).Updates(map[string]interface{}{
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

func (r *gormTransactionRepository) Delete(transactionID int) error {
	result := r.db.Delete(&models.Transaction{}, transactionID)
	if result.Error != nil {
		return fmt.Errorf("Transaction can't deleted: %v", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrTransactionNotFound
	}
	return nil
}
