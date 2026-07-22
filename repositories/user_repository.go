package repositories

import (
	"GoGinMoneyCopilot/models"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

var ErrUsernameTaken = errors.New("Username Aldready Exist!")
var ErrUserNotFound = errors.New("User Not Found!")

type UserRepository interface {
	Create(username, passwordHash string) error
	GetByUsername(username string) (*models.User, error)
	GetByID(userID int) (*models.User, error)
}

type gormUserRepository struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) UserRepository {
	return &gormUserRepository{db: db}
}

func (r *gormUserRepository) Create(username, passwordHash string) error {
	user := models.User{Username: username, PasswordHash: passwordHash}
	if err := r.db.Create(&user).Error; err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrUsernameTaken
		}
		return fmt.Errorf("User couldn't be created: %v", err)
	}
	return nil
}

func (r *gormUserRepository) GetByUsername(username string) (*models.User, error) {
	var user models.User
	if err := r.db.Where("username = ?", username).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("User Can't Be Fetched: %v", err)
	}
	return &user, nil
}

// GetByID — refresh akışında gerekli.
//
// Yeni access token üretirken kullanıcının rolünü TAZE olarak okuyoruz.
// Refresh token'ın içine rolü gömseydik, admin yetkisi alınmış bir kullanıcı
// refresh token'ı geçerli olduğu sürece admin kalmaya devam ederdi.
func (r *gormUserRepository) GetByID(userID int) (*models.User, error) {
	var user models.User
	if err := r.db.First(&user, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("User Can't Be Fetched: %v", err)
	}
	return &user, nil
}
