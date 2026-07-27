package handlers

import (
	"GoGinMoneyCopilot/validators"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestMain runs once before all tests. We do not open a real database —
// it is unnecessary because the tests use fake repositories. We only:
//   - put gin into test mode (silences debug log noise),
//   - register the "accountname" custom validator (the account input
//     binding depends on it).
func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	validators.RegisterCustomValidators()
	os.Exit(m.Run())
}
