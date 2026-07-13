package models

import "time"

type Transaction struct {
	ID              int       `json:"id"`
	AccountID       int       `json:"account_id"`
	CategoryID      int       `json:"category_id"`
	Amount          float64   `json:"amount"`
	Type            string    `json:"type"`
	Description     string    `json:"description"`
	TransactionDate time.Time `json:"transaction_date"`
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
	TransactionDate time.Time `json:"transaction_date" binding:"required"` // "2026-07-13T00:00:00Z"
}
