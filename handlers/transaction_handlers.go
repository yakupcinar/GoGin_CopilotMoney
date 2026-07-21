package handlers

import (
	"GoGinMoneyCopilot/models"
	"GoGinMoneyCopilot/repositories"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

type TransactionHandler struct {
	transactions repositories.TransactionRepository
	accounts     repositories.AccountRepository
}

func NewTransactionHandler(transactions repositories.TransactionRepository, accounts repositories.AccountRepository) *TransactionHandler {
	return &TransactionHandler{transactions: transactions, accounts: accounts}
}

func (h *TransactionHandler) CreateTransaction(c *gin.Context) {
	var input models.CreateTransactionInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input format!"})
		return
	}

	if _, err := getAccountForRequest(c, h.accounts, input.AccountID); err != nil {
		if errors.Is(err, repositories.ErrAccountNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Account not Found"})
			return
		}
		respondInternalError(c, err)
		return
	}

	if err := h.transactions.Create(input); err != nil {
		respondInternalError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "Transaction created!"})
}

func (h *TransactionHandler) GetTransaction(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID Format"})
		return
	}

	tx, err := h.transactions.GetByID(id)
	if err != nil {
		if errors.Is(err, repositories.ErrTransactionNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Transaction not Found"})
			return
		}
		respondInternalError(c, err)
		return
	}

	if _, err := getAccountForRequest(c, h.accounts, tx.AccountID); err != nil {
		if errors.Is(err, repositories.ErrAccountNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Transaction not Found"})
			return
		}
		respondInternalError(c, err)
		return
	}

	c.JSON(http.StatusOK, tx)
}

func (h *TransactionHandler) ListAccountTransactions(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID Format"})
		return
	}

	if _, err := getAccountForRequest(c, h.accounts, id); err != nil {
		if errors.Is(err, repositories.ErrAccountNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Account not Found"})
			return
		}
		respondInternalError(c, err)
		return
	}

	transactions, err := h.transactions.ListByAccount(id)
	if err != nil {
		respondInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, transactions)
}

func (h *TransactionHandler) UpdateTransaction(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID Format"})
		return
	}

	tx, err := h.transactions.GetByID(id)
	if err != nil {
		if errors.Is(err, repositories.ErrTransactionNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Transaction not Found"})
			return
		}
		respondInternalError(c, err)
		return
	}

	if _, err := getAccountForRequest(c, h.accounts, tx.AccountID); err != nil {
		if errors.Is(err, repositories.ErrAccountNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Transaction not Found"})
			return
		}
		respondInternalError(c, err)
		return
	}

	var input models.UpdateTransactionInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input format"})
		return
	}

	if err := h.transactions.Update(id, input); err != nil {
		if errors.Is(err, repositories.ErrTransactionNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Transaction not Found!"})
			return
		}
		respondInternalError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Transaction updated!"})
}

func (h *TransactionHandler) DeleteTransaction(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID Format"})
		return
	}

	tx, err := h.transactions.GetByID(id)
	if err != nil {
		if errors.Is(err, repositories.ErrTransactionNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Transaction not Found"})
			return
		}
		respondInternalError(c, err)
		return
	}

	if _, err := getAccountForRequest(c, h.accounts, tx.AccountID); err != nil {
		if errors.Is(err, repositories.ErrAccountNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Transaction not Found"})
			return
		}
		respondInternalError(c, err)
		return
	}

	if err := h.transactions.Delete(id); err != nil {
		if errors.Is(err, repositories.ErrTransactionNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Transaction not Found"})
			return
		}
		respondInternalError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Transaction deleted!"})
}
