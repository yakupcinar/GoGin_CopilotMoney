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

	if err := h.transactions.Create(c.Request.Context(), input); err != nil {
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

	tx, err := h.transactions.GetByID(c.Request.Context(), id)
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

// ListAccountTransactions — GET /accounts/:id/transactions?page=1&page_size=20
//
// page/page_size verilmezse varsayılana düşer. pageSize üst sınırı (100),
// istemcinin ?page_size=999999 ile tüm tabloyu tek seferde çekmesini engeller.
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

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	transactions, total, err := h.transactions.ListByAccountPaged(c.Request.Context(), id, page, pageSize)
	if err != nil {
		respondInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"transactions": transactions,
		"page":         page,
		"page_size":    pageSize,
		"total":        total,
	})
}

func (h *TransactionHandler) UpdateTransaction(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID Format"})
		return
	}

	tx, err := h.transactions.GetByID(c.Request.Context(), id)
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

	if err := h.transactions.Update(c.Request.Context(), id, input); err != nil {
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

	tx, err := h.transactions.GetByID(c.Request.Context(), id)
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

	if err := h.transactions.Delete(c.Request.Context(), id); err != nil {
		if errors.Is(err, repositories.ErrTransactionNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Transaction not Found"})
			return
		}
		respondInternalError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Transaction deleted!"})
}
