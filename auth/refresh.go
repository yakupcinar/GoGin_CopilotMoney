package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// Refresh token üretimi.
//
// Access token'dan (JWT) farkı: bu OPAK bir değer. İçinde bilgi taşımaz,
// kendi kendini doğrulamaz — anlamı yalnızca veritabanındaki karşılığıdır.
// Bu sayede tek satırlık bir UPDATE ile iptal edilebilir.

// refreshTokenBytes — 32 byte = 256 bit entropi.
// Tahmin edilmesi pratikte imkânsız; bu yüzden hash'lerken bcrypt gibi
// yavaşlatılmış bir algoritmaya gerek yok (bkz. HashRefreshToken).
const refreshTokenBytes = 32

// NewRefreshToken — (ham değer, hash) çifti üretir.
//
// Ham değer YALNIZCA cookie'ye yazılır ve bir daha asla saklanmaz.
// Veritabanına hash'i gider. Böylece DB sızsa bile oturumlar ele geçmez.
func NewRefreshToken() (raw string, hash string, err error) {
	b := make([]byte, refreshTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	// URL-safe base64: cookie değerinde sorun çıkaracak karakter içermez.
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, HashRefreshToken(raw), nil
}

// HashRefreshToken — SHA-256, hex.
//
// NEDEN bcrypt DEĞİL?
// bcrypt kasıtlı olarak yavaştır; amacı düşük entropili parolalarda
// brute-force'u pahalı kılmaktır. Burada değer 256 bit rastgele — brute-force
// zaten imkânsız. Yavaş hash sadece her yenileme isteğini yavaşlatırdı.
func HashRefreshToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
