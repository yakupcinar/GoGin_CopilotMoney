package handlers

import (
	"GoGinMoneyCopilot/validators"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestMain tüm testlerden önce bir kez çalışır. Gerçek veritabanı açmıyoruz —
// fake repository'ler kullandığımız için gerek yok. Sadece:
//   - gin'i test moduna alıyoruz (debug log gürültüsünü kapatır),
//   - "accountname" custom validator'ını kaydediyoruz (hesap input binding'i buna ihtiyaç duyar).
func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	validators.RegisterCustomValidators()
	os.Exit(m.Run())
}
