package auth

import (
	"time"
	"errors"

	"github.com/golang-jwt/jwt/v5"
)

var jwtSecret = []byte("gecici-cok-gizli-anahtar")

func GenerateToken(userID int, isAdmin bool) (string, error) {
	claims := jwt.MapClaims{ //jwt ileride .env'a taşınması gerekir 	
		"user_id": userID,
		"is_admin": isAdmin,
		"exp": time.Now().Add(1 * time.Hour).Unix(), //Tokeni kullanıcı logout olsa bile duruyor bakılacak !
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtSecret)
}

func ValidateToken(tokenString string,) (int, bool, error) {
	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) { // Burada t.Method'un gerçekten HS256 olup olmadığı kontrol edilmiyor. 
		return jwtSecret, nil
	})
	if err != nil || !token.Valid {
		return 0, false, errors.New("Invalid token")
	}
	
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return 0, false, errors.New("Token content hadn't been read")
	}

	userIDFloat, ok := claims["user_id"].(float64)
	if !ok {
		return 0, false, errors.New("User_id not found!")
	}

	isAdmin, _ := claims["is_admin"].(bool)

	return int(userIDFloat), isAdmin, nil
}
