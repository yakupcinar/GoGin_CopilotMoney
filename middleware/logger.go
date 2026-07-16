package middleware

import (
	"encoding/json"
	"log"
	"time"

	"github.com/gin-gonic/gin"
)

type requestLog struct {
	Time      string `json:"time"`
	Method    string `json:"method"`
	Path      string `json:"path"`
	Status    int    `json:"status"`
	Success   bool   `json:"success"`
	LatencyMs int64  `json:"latency_ms"`
	ClientIP  string `json:"client_ip"`
	UserID    *int   `json:"user_id"`
}

func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		var userID *int
		if val, exists := c.Get("user_id"); exists {
			id := val.(int)
			userID = &id
		}

		status := c.Writer.Status()

		entry := requestLog{
			Time:      time.Now().Format(time.RFC3339),
			Method:    c.Request.Method,
			Path:      c.FullPath(),
			Status:    status,
			Success:   status < 400,
			LatencyMs: time.Since(start).Milliseconds(),
			ClientIP:  c.ClientIP(),
			UserID:    userID,
		}

		if data, err := json.Marshal(entry); err == nil {
			log.Println(string(data))
		}
	}
}
