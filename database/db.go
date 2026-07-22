package database

import (
	"GoGinMoneyCopilot/models"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

var DB *gorm.DB

func InitDB() error {
	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		os.Getenv("DB_HOST"),
		os.Getenv("DB_PORT"),
		os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_NAME"),
	)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: dbLogger()})
	if err != nil {
		return fmt.Errorf("connection is unsuccessful: %w", err)
	}
	DB = db
	fmt.Println("Has Been Connected to Database!")

	if err := DB.AutoMigrate(
		&models.User{},
		&models.Account{},
		&models.Category{},
		&models.Transaction{},
		&models.RevokedToken{},
		&models.PendingAction{},
		&models.RefreshToken{},
	); err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}
	return nil
}

// dbLogger — GORM'un log davranışı.
//
// ASIL SORUN: GORM, "kayıt bulunamadı" (ErrRecordNotFound) durumunu varsayılan
// olarak ERROR seviyesinde loglar. Ama bu bizim akışımızda NORMAL bir iş
// sonucudur: olmayan kullanıcı adıyla giriş denemesi, geçersiz onay kodu,
// silinmiş kategori... Üretimde bu satırlar log'u doldurur ve GERÇEK hataları
// gözden kaçırmaya yol açar.
//
// IgnoreRecordNotFoundError=true bunu susturur; gerçek hatalar loglanmaya
// devam eder.
//
// DB_LOG_LEVEL ile ayarlanabilir: silent | error | warn (varsayılan) | info.
// info tüm SQL'i basar — hata ayıklarken faydalı, üretimde değil.
func dbLogger() gormlogger.Interface {
	level := gormlogger.Warn
	switch strings.ToLower(os.Getenv("DB_LOG_LEVEL")) {
	case "silent":
		level = gormlogger.Silent
	case "error":
		level = gormlogger.Error
	case "info":
		level = gormlogger.Info
	}

	return gormlogger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		gormlogger.Config{
			// Bundan uzun süren sorgular uyarı olarak loglanır.
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  level,
			IgnoreRecordNotFoundError: true,
			Colorful:                  true,
		},
	)
}
