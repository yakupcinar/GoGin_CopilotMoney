package models

type Category struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	UserID *int   `json:"user_id"` //UserID neden *int (pointer)? Çünkü DB'de bu alan NULL olabiliyor (global kategori = NULL, kullanıcıya özel = dolu). Go'da normal int hiçbir zaman "boş" olamaz, her zaman 0 gibi bir değeri vardır — NULL'u temsil edebilmek için pointer (*int) kullanıyoruz; nil ise "global kategori" demek oluyor.
}

type CreateCategoryInput struct {
	Name string `json:"name" binding:"required,max=30"`
	Type string `json:"type" binding:"required,oneof=income expense"`
}
