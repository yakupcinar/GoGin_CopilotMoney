package models

import "time"

type Account struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	UserID    int       `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateAccountInput struct {
	Name   string `json:"name" binding:"required,max=30,accountname"` // Yani "Ana Hesap", "Ziraat 2", "Kredi Kartı" gibi gayet normal isimler reddedilecek. Bunu muhtemelen gevşetmen gerekecek (örn. alphanumunicode + boşluğa izin veren custom regex).
}

type UpdateAccountInput struct {
	Name string `json:"name" binding:"required,max=30,accountname"`
}
