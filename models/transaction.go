package models

import "time"

type Transaction struct {
	ID              int       `json:"id" gorm:"primaryKey"`
	AccountID       int       `json:"account_id" gorm:"not null"`
	CategoryID      int       `json:"category_id" gorm:"not null"`
	Amount          float64   `json:"amount" gorm:"type:numeric(12,2);not null"`
	Type            string    `json:"type" gorm:"size:10;not null;check:type IN ('income','expense')"`
	Description     string    `json:"description" gorm:"size:100"`
	TransactionDate time.Time `json:"transaction_date" gorm:"type:date;not null"`
	CreatedAt       time.Time `json:"created_at"`
}

type CreateTransactionInput struct {
	AccountID       int       `json:"account_id" binding:"required"`
	CategoryID      int       `json:"category_id" binding:"required"`
	Amount          float64   `json:"amount" binding:"required,gt=0"`
	Type            string    `json:"type" binding:"required,oneof=income expense"`
	Description     string    `json:"description" binding:"max=100"`
	TransactionDate time.Time `json:"transaction_date" binding:"required"`
}

type UpdateTransactionInput struct {
	CategoryID      int       `json:"category_id" binding:"required"`
	Amount          float64   `json:"amount" binding:"required,gt=0"`
	Type            string    `json:"type" binding:"required,oneof=income expense"`
	Description     string    `json:"description" binding:"max=100"`
	TransactionDate time.Time `json:"transaction_date" binding:"required"`
}
