package repositories

import (
	"GoGinMoneyCopilot/models"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

var ErrAccountNotFound = errors.New("Account Not Found!")

type AccountRepository interface {
	Create(name string, userID int) error
	GetByID(accountID int) (*models.Account, error)
	GetByIDForUser(accountID, userID int) (*models.Account, error)
	Update(accountID int, name string) error
	Delete(accountID int) error
}

type gormAccountRepository struct {
	db *gorm.DB
}

func NewAccountRepository(db *gorm.DB) AccountRepository {
	return &gormAccountRepository{db: db}
}

func (r *gormAccountRepository) Create(name string, userID int) error {
	acc := models.Account{Name: name, UserID: userID}
	if err := r.db.Create(&acc).Error; err != nil {
		return fmt.Errorf("Account couldn't be created: %v", err)
	}
	return nil
}

func (r *gormAccountRepository) GetByID(accountID int) (*models.Account, error) {
	var acc models.Account
	if err := r.db.First(&acc, accountID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAccountNotFound
		}
		return nil, fmt.Errorf("Account Couldn't Be Fetched: %v", err)
	}
	return &acc, nil
}

func (r *gormAccountRepository) GetByIDForUser(accountID, userID int) (*models.Account, error) {
	var acc models.Account
	if err := r.db.Where("id = ? AND user_id = ?", accountID, userID).First(&acc).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAccountNotFound
		}
		return nil, fmt.Errorf("Account Couldn't Be Fetched: %v", err)
	}
	return &acc, nil
}

func (r *gormAccountRepository) Update(accountID int, name string) error {
	result := r.db.Model(&models.Account{}).Where("id = ?", accountID).Update("name", name)
	if result.Error != nil {
		return fmt.Errorf("Account couldn't be updated: %v", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrAccountNotFound
	}
	return nil
}

func (r *gormAccountRepository) Delete(accountID int) error {
	result := r.db.Delete(&models.Account{}, accountID)
	if result.Error != nil {
		return fmt.Errorf("Account can't deleted: %v", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrAccountNotFound
	}
	return nil
}
