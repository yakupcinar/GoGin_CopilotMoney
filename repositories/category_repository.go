package repositories

import (
	"GoGinMoneyCopilot/models"
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

var ErrCategoryNotFound = errors.New("Category Not Found!")
var ErrCategoryInUse = errors.New("Category Is In Use!")

type CategoryRepository interface {
	Create(ctx context.Context, name, categoryType string, userID *int) error
	GetForUser(ctx context.Context, userID int) ([]models.Category, error)
	GetByID(ctx context.Context, categoryID int) (*models.Category, error)
	Update(ctx context.Context, categoryID int, name, categoryType string) error
	Delete(ctx context.Context, categoryID int) error
}

type gormCategoryRepository struct {
	db *gorm.DB
}

func NewCategoryRepository(db *gorm.DB) CategoryRepository {
	return &gormCategoryRepository{db: db}
}

func (r *gormCategoryRepository) Create(ctx context.Context, name, categoryType string, userID *int) error {
	cat := models.Category{Name: name, Type: categoryType, UserID: userID}
	if err := r.db.WithContext(ctx).Create(&cat).Error; err != nil {
		return fmt.Errorf("Category couldn't be created: %v", err)
	}
	return nil
}

func (r *gormCategoryRepository) GetForUser(ctx context.Context, userID int) ([]models.Category, error) {
	var categories []models.Category
	if err := r.db.WithContext(ctx).Where("user_id IS NULL OR user_id = ?", userID).Find(&categories).Error; err != nil {
		return nil, fmt.Errorf("Categories couldn't be fetched: %v", err)
	}
	return categories, nil
}

func (r *gormCategoryRepository) GetByID(ctx context.Context, categoryID int) (*models.Category, error) {
	var cat models.Category
	if err := r.db.WithContext(ctx).First(&cat, categoryID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrCategoryNotFound
		}
		return nil, fmt.Errorf("Category Couldn't Be Fetched: %v", err)
	}
	return &cat, nil
}

func (r *gormCategoryRepository) Update(ctx context.Context, categoryID int, name, categoryType string) error {
	result := r.db.WithContext(ctx).Model(&models.Category{}).Where("id = ?", categoryID).Updates(map[string]interface{}{
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

func (r *gormCategoryRepository) Delete(ctx context.Context, categoryID int) error {
	result := r.db.WithContext(ctx).Delete(&models.Category{}, categoryID)
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
