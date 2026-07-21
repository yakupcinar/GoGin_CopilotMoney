package models

import "time"

type Account struct {
	ID        int       `json:"id" gorm:"primaryKey"`
	Name      string    `json:"name" gorm:"size:16;not null"`
	UserID    int       `json:"user_id" gorm:"not null"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateAccountInput struct {
	Name string `json:"name" binding:"required,max=30,accountname"`
}

type UpdateAccountInput struct {
	Name string `json:"name" binding:"required,max=30,accountname"`
}
