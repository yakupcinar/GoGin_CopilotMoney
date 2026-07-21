package models

type Category struct {
	ID     int    `json:"id" gorm:"primaryKey"`
	Name   string `json:"name" gorm:"size:30;not null"`
	Type   string `json:"type" gorm:"size:10;not null;check:type IN ('income','expense')"`
	UserID *int   `json:"user_id"`
}

type CreateCategoryInput struct {
	Name string `json:"name" binding:"required,max=30"`
	Type string `json:"type" binding:"required,oneof=income expense"`
}

type UpdateCategoryInput struct {
	Name string `json:"name" binding:"required,max=30"`
	Type string `json:"type" binding:"required,oneof=income expense"`
}
