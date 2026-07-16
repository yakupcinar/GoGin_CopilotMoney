package handlers

import (
	"GoGinMoneyCopilot/database"
	"GoGinMoneyCopilot/models"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func CreateAccount(c *gin.Context) {
	var input models.CreateAccountInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input format!"})
		return
	}

	userID := c.MustGet("user_id").(int)

	if err := database.CreateAccount(input.Name, userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"message": "Account created!",
		"name":    input.Name,
	})
}

func GetAccount(c *gin.Context) {
	idParam := c.Param("id")
	id, err := strconv.Atoi(idParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID Format"})
		return
	}

	acc, err := database.GetAccount(id)
	if err != nil {
		if errors.Is(err, database.ErrAccountNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Account not Found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	userID := c.MustGet("user_id").(int)
	isAdmin := c.MustGet("is_admin").(bool)

	if acc.UserID != userID && !isAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "You don't have right to manage other accounts"})
		return
	}
	c.JSON(http.StatusOK, acc)
}

func UpdateAccount(c *gin.Context) {
	idParam := c.Param("id")
	id, err := strconv.Atoi(idParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID Format"})
		return
	}

	acc, err := database.GetAccount(id)
	if err != nil {
		if errors.Is(err, database.ErrAccountNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Account not Found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	userID := c.MustGet("user_id").(int)
	isAdmin := c.MustGet("is_admin").(bool)

	if acc.UserID != userID && !isAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "You are not Admin!"})
		return
	}

	var input models.UpdateAccountInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input format"})
		return
	}

	if err := database.UpdateAccount(id, input.Name); err != nil {
		if errors.Is(err, database.ErrAccountNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Account not Found!"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Account updated!"})
}

func DeleteAccount(c *gin.Context) {
	idParam := c.Param("id")
	id, err := strconv.Atoi(idParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID Format"})
		return
	}

	acc, err := database.GetAccount(id)
	if err != nil {
		if errors.Is(err, database.ErrAccountNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Account not Found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	userID := c.MustGet("user_id").(int)
	isAdmin := c.MustGet("is_admin").(bool)

	if acc.UserID != userID && !isAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "You are not Admin!"})
		return
	}

	if err := database.DeleteAccount(id); err != nil {
		if errors.Is(err, database.ErrAccountNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Account not Found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Account deleted!"})
}
