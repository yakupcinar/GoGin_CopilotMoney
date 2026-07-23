package middleware

import (
	"GoGinMoneyCopilot/auth"
	"GoGinMoneyCopilot/repositories"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func AuthMiddleware(tokens repositories.TokenRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Token not found!"})
			c.Abort()
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Token format invalid"})
			c.Abort()
			return
		}

		claims, err := auth.ValidateToken(parts[1])
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
			c.Abort()
			return
		}

		revoked, err := tokens.IsRevoked(claims.JTI)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Token couldn't be verified"})
			c.Abort()
			return
		}
		if revoked {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Token revoked"})
			c.Abort()
			return
		}

		c.Set("user_id", claims.UserID)
		c.Set("role", claims.Role)
		c.Set("jti", claims.JTI)
		c.Set("token_exp", claims.Expires)
		c.Next()
	}
}
