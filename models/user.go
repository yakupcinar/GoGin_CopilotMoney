package models

import "time"

type User struct {
	ID           int       `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	IsAdmin      bool      `json:"is_admin"` //Bu yöntemden kaçınıp proje büyümesinde Roller için farklı bir middleware açmaya gidebiliriz token üzerinden onaylamak yerine, fazla rol açılabilir. (client, support, admin)
	CreatedAt    time.Time `json:"created_at"`
}

type RegisterInput struct {
	Username string `json:"username" binding:"required,min=3,max=20,alphanum"`
	Password string `json:"password" binding:"required,min=8,max=16"` // Şifre için üst sınır yok — RegisterInput.Password sadece min=8, max yok. Küçük ama ilginç bir detay: bcrypt 72 byte'tan uzun şifreleri sessizce kesiyor, yani çok uzun şifrelerin fazlası hash'e katkı sağlamıyor. 
	// Şifre karmaşıklık kuralı yok (büyük/küçük harf, rakam, sembol zorunluluğu)
}

type LoginInput struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type LoginResponse struct {
	Token string `json:"token"`
}