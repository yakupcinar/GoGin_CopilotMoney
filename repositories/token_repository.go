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
	DeleteExpired(before time.Time) (int64, error)
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

// DeleteExpired — süresi geçmiş iptal kayıtlarını siler.
//
// NEDEN SİLMEK GÜVENLİ?
// Bu tablo "bu access token iptal edildi" listesidir. Süresi dolmuş bir
// token'ı listede tutmanın hiçbir faydası yok: JWT doğrulaması zaten exp
// alanına bakıp reddediyor. Yani kayıt silinse de o token asla çalışmaz.
//
// Silmezsek tablo sonsuza kadar büyür — her logout bir satır ekler.
func (r *gormTokenRepository) DeleteExpired(before time.Time) (int64, error) {
	result := r.db.Where("expires_at < ?", before).Delete(&models.RevokedToken{})
	if result.Error != nil {
		return 0, fmt.Errorf("expired revoked tokens couldn't be deleted: %v", result.Error)
	}
	return result.RowsAffected, nil
}
