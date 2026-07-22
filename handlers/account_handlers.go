package handlers

import (
	"GoGinMoneyCopilot/models"
	"GoGinMoneyCopilot/repositories"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

type AccountHandler struct {
	accounts repositories.AccountRepository
}

func NewAccountHandler(accounts repositories.AccountRepository) *AccountHandler {
	return &AccountHandler{accounts: accounts}
}

func (h *AccountHandler) CreateAccount(c *gin.Context) {
	var input models.CreateAccountInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input format!"})
		return
	}

	userID := c.MustGet("user_id").(int)

	if err := h.accounts.Create(input.Name, userID); err != nil {
		respondInternalError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"message": "Account created!",
		"name":    input.Name,
	})
}

func (h *AccountHandler) GetAccount(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID Format"})
		return
	}

	acc, err := getAccountForRequest(c, h.accounts, id)
	if err != nil {
		if errors.Is(err, repositories.ErrAccountNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Account not Found"})
			return
		}
		respondInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, acc)
}

func (h *AccountHandler) UpdateAccount(c *gin.Context) {
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

	var input models.UpdateAccountInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input format"})
		return
	}

	if err := h.accounts.Update(id, input.Name); err != nil {
		if errors.Is(err, repositories.ErrAccountNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Account not Found!"})
			return
		}
		respondInternalError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Account updated!"})
}

func (h *AccountHandler) DeleteAccount(c *gin.Context) {
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

	if err := h.accounts.Delete(id); err != nil {
		if errors.Is(err, repositories.ErrAccountNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Account not Found"})
			return
		}
		if errors.Is(err, repositories.ErrAccountInUse) {
			c.JSON(http.StatusConflict, gin.H{
				"error": "This account has existing transactions and cannot be deleted"})
			return
		}
		respondInternalError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Account deleted!"})
}
