package repositories

import (
	"GoGinMoneyCopilot/models"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TokenRepository interface {
	Revoke(jti string, expiresAt time.Time) error
	IsRevoked(jti string) (bool, error)
}

type gormTokenRepository struct {
	db *gorm.DB
}

func NewTokenRepository(db *gorm.DB) TokenRepository {
	return &gormTokenRepository{db: db}
}

func (r *gormTokenRepository) Revoke(jti string, expiresAt time.Time) error {
	token := models.RevokedToken{JTI: jti, ExpiresAt: expiresAt}
	if err := r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&token).Error; err != nil {
		return fmt.Errorf("token couldn't be revoked: %v", err)
	}
	return nil
}

func (r *gormTokenRepository) IsRevoked(jti string) (bool, error) {
	var count int64
	if err := r.db.Model(&models.RevokedToken{}).Where("jti = ?", jti).Count(&count).Error; err != nil {
		return false, fmt.Errorf("revocation check failed: %v", err)
	}
	return count > 0, nil
}
