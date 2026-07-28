package models

import "time"

type Role string

const (
	RoleClient Role = "client"
	RoleAdmin  Role = "admin"
)

// Plan — abonelik katmanı. Yetki (Role) ile karıştırılmamalı: Role "ne
// yapabilirsin" sorusuna, Plan "ne kadar kullanabilirsin" sorusuna cevap
// verir. Şu an tek kullanım yeri /chat rate limitidir (chat/service.go her
// istekte gerçek para harcıyor); ileride model seçimi, aylık kota gibi başka
// alanlara da genişleyebilir.
type Plan string

const (
	PlanFree Plan = "free"
	PlanPro  Plan = "pro"
)

type User struct {
	ID           int       `json:"id" gorm:"primaryKey"`
	Username     string    `json:"username" gorm:"size:20;unique;not null"`
	PasswordHash string    `json:"-" gorm:"column:password_hash;not null"`
	Role         Role      `json:"role" gorm:"size:10;not null;default:client"`
	Plan         Plan      `json:"plan" gorm:"size:10;not null;default:free"`
	CreatedAt    time.Time `json:"created_at"`
}

type RegisterInput struct {
	Username string `json:"username" binding:"required,min=3,max=20,alphanum"`
	Password string `json:"password" binding:"required,min=8,max=30"`
}

type LoginInput struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type LoginResponse struct {
	Token string `json:"token"`
}
