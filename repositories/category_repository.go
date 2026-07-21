package repositories

import (
	"GoGinMoneyCopilot/models"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

var ErrCategoryNotFound = errors.New("Category Not Found!")
var ErrCategoryInUse = errors.New("Category Is In Use!")

type CategoryRepository interface {
	Create(name, categoryType string, userID *int) error
	GetForUser(userID int) ([]models.Category, error)
	GetByID(categoryID int) (*models.Category, error)
	Update(categoryID int, name, categoryType string) error
	Delete(categoryID int) error
}

type gormCategoryRepository struct {
	db *gorm.DB
}

func NewCategoryRepository(db *gorm.DB) CategoryRepository {
	return &gormCategoryRepository{db: db}
}

func (r *gormCategoryRepository) Create(name, categoryType string, userID *int) error {
	cat := models.Category{Name: name, Type: categoryType, UserID: userID}
	if err := r.db.Create(&cat).Error; err != nil {
		return fmt.Errorf("Category couldn't be created: %v", err)
	}
	return nil
}

func (r *gormCategoryRepository) GetForUser(userID int) ([]models.Category, error) {
	var categories []models.Category
	if err := r.db.Where("user_id IS NULL OR user_id = ?", userID).Find(&categories).Error; err != nil {
		return nil, fmt.Errorf("Categories couldn't be fetched: %v", err)
	}
	return categories, nil
}

func (r *gormCategoryRepository) GetByID(categoryID int) (*models.Category, error) {
	var cat models.Category
	if err := r.db.First(&cat, categoryID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrCategoryNotFound
		}
		return nil, fmt.Errorf("Category Couldn't Be Fetched: %v", err)
	}
	return &cat, nil
}

func (r *gormCategoryRepository) Update(categoryID int, name, categoryType string) error {
	result := r.db.Model(&models.Category{}).Where("id = ?", categoryID).Updates(map[string]interface{}{
		"name": name,
		"type": categoryType,
	})
	if result.Error != nil {
		return fmt.Errorf("Category couldn't be updated: %v", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrCategoryNotFound
	}
	return nil
}

func (r *gormCategoryRepository) Delete(categoryID int) error {
	result := r.db.Delete(&models.Category{}, categoryID)
	if result.Error != nil {
		var pgErr *pgconn.PgError
		if errors.As(result.Error, &pgErr) && pgErr.Code == "23503" {
			return ErrCategoryInUse
		}
		return fmt.Errorf("Category can't deleted: %v", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrCategoryNotFound
	}
	return nil
}
