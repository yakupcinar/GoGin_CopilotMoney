package handlers

import (
	"GoGinMoneyCopilot/models"
	"GoGinMoneyCopilot/repositories"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

func respondInternalError(c *gin.Context, err error) {
	log.Println("internal error:", err)
	c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
}

// getAccountForRequest fetches an account scoped to the requester.
// Admins may access any account; regular users only their own — ownership is
// enforced at the query level, so a non-admin asking for someone else's account
// gets ErrAccountNotFound rather than leaking its existence.
func getAccountForRequest(c *gin.Context, accounts repositories.AccountRepository, accountID int) (*models.Account, error) {
	role := c.MustGet("role").(models.Role)
	if role == models.RoleAdmin {
		return accounts.GetByID(accountID)
	}
	userID := c.MustGet("user_id").(int)
	return accounts.GetByIDForUser(accountID, userID)
}
