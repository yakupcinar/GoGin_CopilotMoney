package auth

import (
	"GoGinMoneyCopilot/models"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func jwtSecret() []byte {
	return []byte(os.Getenv("JWT_SECRET"))
}

func generateJTI() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

type TokenClaims struct {
	UserID  int
	Role    models.Role
	JTI     string
	Expires time.Time
}

func GenerateToken(userID int, role models.Role) (string, error) {
	jti, err := generateJTI()
	if err != nil {
		return "", err
	}
	claims := jwt.MapClaims{
		"user_id": userID,
		"role":    string(role),
		"jti":     jti,
		// Ömür ACCESS_TOKEN_TTL ile ayarlanır (varsayılan 15 dk).
		// Kısa tutuluyor: access token bellekte/header'da taşındığı için XSS'e
		// görece açık; ömrü kısaltarak çalınması hâlindeki pencereyi daraltıyoruz.
		// Oturum sürekliliğini refresh token sağlıyor.
		"exp": time.Now().Add(AccessTokenTTL()).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtSecret())
}

func ValidateToken(tokenString string) (*TokenClaims, error) {
	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
		return jwtSecret(), nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !token.Valid {
		return nil, errors.New("invalid token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("token content hadn't been read")
	}

	userIDFloat, ok := claims["user_id"].(float64)
	if !ok {
		return nil, errors.New("user_id not found")
	}
	jti, ok := claims["jti"].(string)
	if !ok {
		return nil, errors.New("jti not found")
	}
	expFloat, ok := claims["exp"].(float64)
	if !ok {
		return nil, errors.New("exp not found")
	}
	roleStr, _ := claims["role"].(string)

	return &TokenClaims{
		UserID:  int(userIDFloat),
		Role:    models.Role(roleStr),
		JTI:     jti,
		Expires: time.Unix(int64(expFloat), 0),
	}, nil
}

// token invalid çek süre kontrolü. verilen süre öncesi expire olmadan
//Tokeni kullanıcı logout olsa bile duruyor bakılacak !
// Burada t.Method'un gerçekten HS256 olup olmadığı kontrol edilmiyor. s
