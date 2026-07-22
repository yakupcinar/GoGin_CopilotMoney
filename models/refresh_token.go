package models

import "time"

// RefreshToken — uzun ömürlü oturum kaydı.
//
// NEDEN JWT DEĞİL?
// JWT kendi kendini doğrular; iptal etmek için yine bir kara listeye bakmak
// gerekir. Zaten DB'ye yazacaksak, opak rastgele bir değer daha basit ve
// taklit edilemez. Access token JWT kalıyor (kısa ömürlü, doğrulaması ucuz).
//
// NEDEN HASH SAKLIYORUZ?
// DB sızarsa oturumlar ele geçmesin. TokenHash = SHA-256(ham değer).
// bcrypt DEĞİL: bcrypt düşük entropili parolalar için yavaşlatılmıştır.
// Buradaki değer zaten 256 bit rastgele — brute-force anlamsız, SHA-256 yeterli.
type RefreshToken struct {
	ID     int `json:"id" gorm:"primaryKey"`
	UserID int `json:"user_id" gorm:"not null;index"`

	// TokenHash — ham token ASLA saklanmaz. json:"-" ile serileştirmeye de kapalı.
	TokenHash string `json:"-" gorm:"size:64;uniqueIndex;not null"`

	ExpiresAt time.Time `json:"expires_at" gorm:"not null;index"`

	// UsedAt — rotasyonda tüketildi. Dolu bir token tekrar sunulursa
	// bu bir SIZINTI işaretidir (bkz. reuse detection).
	UsedAt *time.Time `json:"used_at"`

	// RevokedAt — logout ya da sızıntı tespiti sonrası iptal.
	RevokedAt *time.Time `json:"revoked_at"`

	CreatedAt time.Time `json:"created_at"`
}

// IsUsable — token'ın yenileme için kullanılabilir olup olmadığı.
// DİKKAT: bu yalnızca alanları okur. Gerçek tüketim ATOMİK olmalıdır
// (repository.Consume) — yoksa iki eşzamanlı istek ikisi de geçebilir.
func (t *RefreshToken) IsUsable(now time.Time) bool {
	return t.UsedAt == nil && t.RevokedAt == nil && now.Before(t.ExpiresAt)
}
