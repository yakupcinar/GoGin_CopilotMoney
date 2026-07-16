package handlers

import (
	"GoGinMoneyCopilot/database"
	"GoGinMoneyCopilot/models"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func CreateTransaction(c *gin.Context) {
	var input models.CreateTransactionInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input format!"})
		return
	}

	acc, err := database.GetAccount(input.AccountID)
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

	if err := database.CreateTransaction(input); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "Transaction created!"})
}

func GetTransaction(c *gin.Context) {
	idParam := c.Param("id")
	id, err := strconv.Atoi(idParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID Format"})
		return
	}

	tx, err := database.GetTransaction(id)
	if err != nil {
		if errors.Is(err, database.ErrTransactionNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Transaction not Found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	acc, err := database.GetAccount(tx.AccountID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	userID := c.MustGet("user_id").(int)
	isAdmin := c.MustGet("is_admin").(bool)

	if acc.UserID != userID && !isAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "You don't have right to manage other accounts"})
		return
	}

	c.JSON(http.StatusOK, tx)
}

func ListAccountTransactions(c *gin.Context) {
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

	transactions, err := database.ListTransactionsByAccount(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, transactions)
}

func UpdateTransaction(c *gin.Context) {
	idParam := c.Param("id")
	id, err := strconv.Atoi(idParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID Format"})
		return
	}

	tx, err := database.GetTransaction(id)
	if err != nil {
		if errors.Is(err, database.ErrTransactionNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Transaction not Found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	acc, err := database.GetAccount(tx.AccountID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	userID := c.MustGet("user_id").(int)
	isAdmin := c.MustGet("is_admin").(bool)

	if acc.UserID != userID && !isAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "You are not Admin!"})
		return
	}

	var input models.UpdateTransactionInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input format"})
		return
	}

	if err := database.UpdateTransaction(id, input); err != nil {
		if errors.Is(err, database.ErrTransactionNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Transaction not Found!"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Transaction updated!"})
}

func DeleteTransaction(c *gin.Context) {
	idParam := c.Param("id")
	id, err := strconv.Atoi(idParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID Format"})
		return
	}

	tx, err := database.GetTransaction(id)
	if err != nil {
		if errors.Is(err, database.ErrTransactionNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Transaction not Found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	acc, err := database.GetAccount(tx.AccountID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	userID := c.MustGet("user_id").(int)
	isAdmin := c.MustGet("is_admin").(bool)

	if acc.UserID != userID && !isAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "You are not Admin!"})
		return
	}

	if err := database.DeleteTransaction(id); err != nil {
		if errors.Is(err, database.ErrTransactionNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Transaction not Found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Transaction deleted!"})
}
