package repositories

import (
	"GoGinMoneyCopilot/models"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

var ErrBudgetNotFound = errors.New("Budget Not Found!")
var ErrBudgetExists = errors.New("Budget Already Exists!")

// BudgetRepository — budgets + budget_categories tablolarının ikisine birden
// sahiptir. Bu, "her repo tek tablo" kuralının bilinçli istisnası: başlık ve
// satırlar her zaman BİRLİKTE yazılır/silinir, yani tek bir bütünü oluşturur.
//
// SAHİPLİK: GetForUser sahipliği sorgunun ANAHTARI yapar (WHERE user_id = ?).
// Kullanıcı başına tek bütçe olduğu için hiçbir URL'de bütçe id'si geçmez;
// dolayısıyla bu özellikte yanlış yapılabilecek bir çapraz-kullanıcı kod yolu
// yoktur. Replace/Delete'e giden budgetID her zaman handler'ın az önce yaptığı
// bir GetForUser'dan gelir.
type BudgetRepository interface {
	Create(userID int, input models.CreateBudgetInput, startDate time.Time) error
	GetForUser(userID int) (*models.Budget, error)
	ListCategories(budgetID int) ([]models.BudgetCategory, error)
	Replace(budgetID int, input models.UpdateBudgetInput, startDate time.Time) error
	Delete(budgetID int) error
	CountByCategory(categoryID int) (int64, error)
}

type gormBudgetRepository struct {
	db *gorm.DB
}

func NewBudgetRepository(db *gorm.DB) BudgetRepository {
	return &gormBudgetRepository{db: db}
}

// linesFor — girdi satırlarını kayıt satırlarına çevirir.
func linesFor(budgetID int, inputs []models.BudgetCategoryInput) []models.BudgetCategory {
	lines := make([]models.BudgetCategory, 0, len(inputs))
	for _, in := range inputs {
		lines = append(lines, models.BudgetCategory{
			BudgetID:    budgetID,
			CategoryID:  in.CategoryID,
			LimitAmount: in.LimitAmount,
		})
	}
	return lines
}

// Create — başlık ve satırlar TEK TRANSACTION'da yazılır.
//
// NEDEN TRANSACTION (projede ilk kez kullanılıyor): satırlar yazılmadan başlık
// kalırsa bütçe "toplam limit 0" olarak görünür. Patlamaz — sessizce yanlış
// olur, ki bu en kötüsüdür.
func (r *gormBudgetRepository) Create(userID int, input models.CreateBudgetInput, startDate time.Time) error {
	err := r.db.Transaction(func(tx *gorm.DB) error {
		budget := models.Budget{
			UserID:     userID,
			Name:       input.Name,
			StartDate:  models.CivilDate(startDate),
			PeriodDays: input.PeriodDays,
		}
		if err := tx.Create(&budget).Error; err != nil {
			return err
		}
		return tx.Omit("Category").Create(linesFor(budget.ID, input.Categories)).Error
	})
	if err != nil {
		// uniqueIndex(user_id) ihlali: kullanıcının zaten bir bütçesi var.
		// Bu bir ÇAKIŞMADIR (409), sunucu arızası değil — category_repository
		// içindeki 23503 işlemesiyle aynı gerekçe.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrBudgetExists
		}
		return fmt.Errorf("Budget couldn't be created: %v", err)
	}
	return nil
}

func (r *gormBudgetRepository) GetForUser(userID int) (*models.Budget, error) {
	var budget models.Budget
	if err := r.db.Where("user_id = ?", userID).First(&budget).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrBudgetNotFound
		}
		return nil, fmt.Errorf("Budget Couldn't Be Fetched: %v", err)
	}
	return &budget, nil
}

func (r *gormBudgetRepository) ListCategories(budgetID int) ([]models.BudgetCategory, error) {
	var lines []models.BudgetCategory
	if err := r.db.Where("budget_id = ?", budgetID).Order("id").Find(&lines).Error; err != nil {
		return nil, fmt.Errorf("Budget categories couldn't be fetched: %v", err)
	}
	return lines, nil
}

// Replace — TAM DEĞİŞTİRME: başlık güncellenir, eski satırlar silinip yenileri
// yazılır. Hepsi tek transaction'da; yarı yazılmış bir bütçe kalamaz.
func (r *gormBudgetRepository) Replace(budgetID int, input models.UpdateBudgetInput, startDate time.Time) error {
	err := r.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.Budget{}).Where("id = ?", budgetID).Updates(map[string]interface{}{
			"name":        input.Name,
			"start_date":  models.CivilDate(startDate),
			"period_days": input.PeriodDays,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrBudgetNotFound
		}
		if err := tx.Where("budget_id = ?", budgetID).Delete(&models.BudgetCategory{}).Error; err != nil {
			return err
		}
		return tx.Omit("Category").Create(linesFor(budgetID, input.Categories)).Error
	})
	if err != nil {
		if errors.Is(err, ErrBudgetNotFound) {
			return err
		}
		return fmt.Errorf("Budget couldn't be updated: %v", err)
	}
	return nil
}

// Delete — satırlar ELLE siliniyor, ON DELETE CASCADE'e güvenilmiyor:
// AutoMigrate bu projede hiçbir FK kısıtı üretmez (hiçbir modelde foreignKey
// etiketi yok), dolayısıyla cascade diye bir şey yoktur.
func (r *gormBudgetRepository) Delete(budgetID int) error {
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("budget_id = ?", budgetID).Delete(&models.BudgetCategory{}).Error; err != nil {
			return err
		}
		result := tx.Delete(&models.Budget{}, budgetID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrBudgetNotFound
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrBudgetNotFound) {
			return err
		}
		return fmt.Errorf("Budget can't deleted: %v", err)
	}
	return nil
}

// CountByCategory — bir kategoriye kaç bütçe satırı bağlı. Kategori silmeden
// önce 409 kararı için kullanılır; TransactionRepository.CountByCategory ile
// bilerek aynı isim ve şekilde.
func (r *gormBudgetRepository) CountByCategory(categoryID int) (int64, error) {
	var count int64
	if err := r.db.Model(&models.BudgetCategory{}).
		Where("category_id = ?", categoryID).Count(&count).Error; err != nil {
		return 0, fmt.Errorf("Budget category count couldn't be fetched: %v", err)
	}
	return count, nil
}
