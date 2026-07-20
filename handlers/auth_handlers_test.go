// handlers/auth_handlers_test.go
package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestLoginWrongPassword(t *testing.T) {
	r := gin.New()
	r.POST("/login", Login)

	body := `{"username":"testuser1","password":"wrongpassword"}`
	req := httptest.NewRequest("POST", "/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expeceted 401, came %d", w.Code)
	}
}
