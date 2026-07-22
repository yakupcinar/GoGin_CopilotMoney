package repositories

import (
	"GoGinMoneyCopilot/models"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// Onay kodu geçersiz. Sebebi BİLEREK ayrıştırılmıyor — aşağıdaki nota bak.
var ErrPendingActionInvalid = errors.New("Confirmation Token Invalid!")

type PendingActionRepository interface {
	Create(action *models.PendingAction) error
	Claim(userID int, token string, now time.Time) (*models.PendingAction, error)
	DeleteExpired(before time.Time) (int64, error)
}

type gormPendingActionRepository struct {
	db *gorm.DB
}

func NewPendingActionRepository(db *gorm.DB) PendingActionRepository {
	return &gormPendingActionRepository{db: db}
}

func (r *gormPendingActionRepository) Create(action *models.PendingAction) error {
	if err := r.db.Create(action).Error; err != nil {
		return fmt.Errorf("Pending action couldn't be created: %v", err)
	}
	return nil
}

// Claim — token'ı doğrular ve ATOMİK olarak "kullanıldı" işaretler.
//
// NEDEN TEK SORGU?
// Playground'da bellekte mutex ile koruyorduk; tek süreçte yeterliydi.
// Veritabanında mutex yetmez — mutex sadece kendi sunucu kopyanı kilitler,
// ikinci kopya aynı satırı aynı anda okuyabilir.
//
// Yanlış yol:
//
//	action := SELECT ... WHERE token = ?   // iki istek de "kullanılmamış" görür
//	if action.UsedAt == nil { UPDATE ... } // iki istek de siler
//
// Doğru yol: koşulu UPDATE'in WHERE'ine koymak. Veritabanı satır kilidini
// kendisi yönetir; iki istek gelirse YALNIZCA biri RowsAffected=1 alır.
func (r *gormPendingActionRepository) Claim(userID int, token string, now time.Time) (*models.PendingAction, error) {
	result := r.db.Model(&models.PendingAction{}).
		Where("token = ? AND user_id = ? AND used_at IS NULL AND expires_at > ?",
			token, userID, now).
		Update("used_at", now)

	if result.Error != nil {
		return nil, fmt.Errorf("Pending action couldn't be claimed: %v", result.Error)
	}
	if result.RowsAffected == 0 {
		// Dört farklı sebepten olabilir: yok / başkasının / kullanılmış /
		// süresi dolmuş. Hepsine AYNI hatayı dönüyoruz.
		//
		// Ayırsaydık, başkasının token'ını deneyen biri "bu token var ama
		// süresi dolmuş" bilgisini elde ederdi. Login'deki "kullanıcı adı
		// veya şifre yanlış" mantığının aynısı.
		return nil, ErrPendingActionInvalid
	}

	var action models.PendingAction
	if err := r.db.Where("token = ?", token).First(&action).Error; err != nil {
		return nil, fmt.Errorf("Pending action couldn't be fetched: %v", err)
	}
	return &action, nil
}

// DeleteExpired — süresi geçmiş kayıtları temizler.
// Tablo sonsuza kadar büyümesin diye periyodik çağrılmalı
// (revoked_tokens tablosunda da aynı ihtiyaç var).
func (r *gormPendingActionRepository) DeleteExpired(before time.Time) (int64, error) {
	result := r.db.Where("expires_at < ?", before).Delete(&models.PendingAction{})
	if result.Error != nil {
		return 0, fmt.Errorf("Expired pending actions couldn't be deleted: %v", result.Error)
	}
	return result.RowsAffected, nil
}
