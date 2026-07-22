package repositories

import (
	"GoGinMoneyCopilot/models"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

var (
	// ErrRefreshTokenInvalid — yok / süresi dolmuş / iptal edilmiş.
	ErrRefreshTokenInvalid = errors.New("Refresh Token Invalid!")

	// ErrRefreshTokenReused — token DAHA ÖNCE tüketilmiş ve tekrar sunuldu.
	// Bu bir SIZINTI işaretidir: ya saldırgan çaldı ya da meşru kullanıcı
	// eski bir token'ı tekrar oynatıyor. Çağıran taraf kullanıcının TÜM
	// refresh token'larını iptal etmelidir.
	//
	// DİKKAT: bu ayrım SUNUCU İÇİNDİR. Client'a her iki durumda da aynı
	// jenerik 401 dönmelidir — aksi halde saldırgan "bu token gerçekti ama
	// kullanılmış" bilgisini elde eder.
	ErrRefreshTokenReused = errors.New("Refresh Token Reused!")
)

type RefreshTokenRepository interface {
	Create(token *models.RefreshToken) error
	// Consume — ATOMİK tüketim. ErrRefreshTokenReused durumunda kaydı DA döner,
	// çünkü çağıranın o kullanıcının tüm oturumlarını iptal etmesi gerekir.
	Consume(tokenHash string, now time.Time) (*models.RefreshToken, error)
	Revoke(tokenHash string, now time.Time) error
	RevokeAllForUser(userID int, now time.Time) error
	DeleteExpired(before time.Time) (int64, error)
}

type gormRefreshTokenRepository struct {
	db *gorm.DB
}

func NewRefreshTokenRepository(db *gorm.DB) RefreshTokenRepository {
	return &gormRefreshTokenRepository{db: db}
}

func (r *gormRefreshTokenRepository) Create(token *models.RefreshToken) error {
	if err := r.db.Create(token).Error; err != nil {
		return fmt.Errorf("Refresh token couldn't be created: %v", err)
	}
	return nil
}

// Consume — token'ı doğrular ve ATOMİK olarak tüketilmiş işaretler (rotasyon).
//
// NEDEN TEK SORGU?
// pending_repository.Claim ile aynı gerekçe: önce SELECT sonra UPDATE yaparsak
// iki eşzamanlı yenileme isteği ikisi de "kullanılmamış" görüp ikisi de yeni
// token üretir. Koşulu UPDATE'in WHERE'ine koyunca veritabanı satır kilidini
// kendisi yönetir; yalnızca biri RowsAffected=1 alır.
//
// RowsAffected == 0 ise sebebini AYRICA sorguluyoruz — çünkü "tüketilmiş token
// tekrar geldi" durumu diğerlerinden farklı bir tepki gerektiriyor (tüm
// oturumları iptal et).
func (r *gormRefreshTokenRepository) Consume(tokenHash string, now time.Time) (*models.RefreshToken, error) {
	result := r.db.Model(&models.RefreshToken{}).
		Where("token_hash = ? AND used_at IS NULL AND revoked_at IS NULL AND expires_at > ?",
			tokenHash, now).
		Update("used_at", now)

	if result.Error != nil {
		return nil, fmt.Errorf("Refresh token couldn't be consumed: %v", result.Error)
	}

	if result.RowsAffected == 1 {
		var token models.RefreshToken
		if err := r.db.Where("token_hash = ?", tokenHash).First(&token).Error; err != nil {
			return nil, fmt.Errorf("Refresh token couldn't be fetched: %v", err)
		}
		return &token, nil
	}

	// Tüketemedik. Neden? Sızıntı mı, sıradan geçersizlik mi?
	var existing models.RefreshToken
	if err := r.db.Where("token_hash = ?", tokenHash).First(&existing).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRefreshTokenInvalid
		}
		return nil, fmt.Errorf("Refresh token couldn't be fetched: %v", err)
	}

	// Token GERÇEK ama zaten tüketilmiş -> birileri eski bir token oynatıyor.
	// Kaydı DA döndürüyoruz: çağıran taraf UserID'yi bilmeli ki o kullanıcının
	// tüm oturumlarını iptal edebilsin.
	if existing.UsedAt != nil {
		return &existing, ErrRefreshTokenReused
	}

	// İptal edilmiş ya da süresi dolmuş: sıradan geçersizlik.
	return nil, ErrRefreshTokenInvalid
}

// Revoke — tek bir refresh token'ı iptal eder (logout).
//
// Neden Consume değil de ayrı bir metot? Consume "rotasyon için tüketildi"
// anlamına gelir; logout ise "bu oturum bitti". İkisini ayırmak, ileride
// "aktif oturumlarım" ekranı yapıldığında durumu doğru göstermeyi sağlar.
func (r *gormRefreshTokenRepository) Revoke(tokenHash string, now time.Time) error {
	result := r.db.Model(&models.RefreshToken{}).
		Where("token_hash = ? AND revoked_at IS NULL", tokenHash).
		Update("revoked_at", now)
	if result.Error != nil {
		return fmt.Errorf("Refresh token couldn't be revoked: %v", result.Error)
	}
	return nil
}

// RevokeAllForUser — kullanıcının tüm aktif refresh token'larını iptal eder.
//
// İki yerde kullanılır:
//  1. Sızıntı tespitinde (ErrRefreshTokenReused) — tüm cihazlardan çıkış
//  2. İstenirse "her yerden çıkış yap" özelliğinde
func (r *gormRefreshTokenRepository) RevokeAllForUser(userID int, now time.Time) error {
	result := r.db.Model(&models.RefreshToken{}).
		Where("user_id = ? AND revoked_at IS NULL", userID).
		Update("revoked_at", now)
	if result.Error != nil {
		return fmt.Errorf("Refresh tokens couldn't be revoked: %v", result.Error)
	}
	return nil
}

// DeleteExpired — süresi geçmiş kayıtları temizler.
// revoked_tokens ve pending_actions tablolarında da aynı ihtiyaç var;
// üçü tek bir periyodik temizlik görevine bağlanmalı.
func (r *gormRefreshTokenRepository) DeleteExpired(before time.Time) (int64, error) {
	result := r.db.Where("expires_at < ?", before).Delete(&models.RefreshToken{})
	if result.Error != nil {
		return 0, fmt.Errorf("Expired refresh tokens couldn't be deleted: %v", result.Error)
	}
	return result.RowsAffected, nil
}
