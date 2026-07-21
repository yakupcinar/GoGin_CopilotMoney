package models

import "time"

type RevokedToken struct {
	JTI       string    `json:"jti" gorm:"primaryKey;column:jti"`
	ExpiresAt time.Time `json:"expires_at" gorm:"not null"`
}
