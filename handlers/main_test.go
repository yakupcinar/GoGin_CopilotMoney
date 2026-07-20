package handlers

import (
	"GoGinMoneyCopilot/database"
	"os"
	"testing"

	"github.com/joho/godotenv"
)

func TestMain(m *testing.M) {
	godotenv.Load("../.env")
	database.InitDB()
	os.Exit(m.Run())
}
