package handlers

import (
	"GoGinMoneyCopilot/models"
	"GoGinMoneyCopilot/repositories"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

type CategoryHandler struct {
	categories repositories.CategoryRepository
	budgets    repositories.BudgetRepository
}

func NewCategoryHandler(categories repositories.CategoryRepository, budgets repositories.BudgetRepository) *CategoryHandler {
	return &CategoryHandler{categories: categories, budgets: budgets}
}

func (h *CategoryHandler) CreateCategory(c *gin.Context) {
	var input models.CreateCategoryInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input format!"})
		return
	}

	userID := c.MustGet("user_id").(int)

	if err := h.categories.Create(input.Name, input.Type, &userID); err != nil {
		respondInternalError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"message": "Category created!",
		"name":    input.Name,
	})
}

func (h *CategoryHandler) ListCategories(c *gin.Context) {
	userID := c.MustGet("user_id").(int)

	categories, err := h.categories.GetForUser(userID)
	if err != nil {
		respondInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, categories)
}

func (h *CategoryHandler) UpdateCategory(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID Format"})
		return
	}

	cat, err := h.categories.GetByID(id)
	if err != nil {
		if errors.Is(err, repositories.ErrCategoryNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Category not Found"})
			return
		}
		respondInternalError(c, err)
		return
	}

	userID := c.MustGet("user_id").(int)
	role := c.MustGet("role").(models.Role)
	isAdmin := role == models.RoleAdmin

	if cat.UserID == nil {
		if !isAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "Global categories can only be modified by an admin"})
			return
		}
	} else if *cat.UserID != userID && !isAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "You don't have right to manage other users' categories"})
		return
	}

	var input models.UpdateCategoryInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input format"})
		return
	}

	if err := h.categories.Update(id, input.Name, input.Type); err != nil {
		if errors.Is(err, repositories.ErrCategoryNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Category not Found!"})
			return
		}
		respondInternalError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Category updated!"})
}

func (h *CategoryHandler) DeleteCategory(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID Format"})
		return
	}

	cat, err := h.categories.GetByID(id)
	if err != nil {
		if errors.Is(err, repositories.ErrCategoryNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Category not Found"})
			return
		}
		respondInternalError(c, err)
		return
	}

	userID := c.MustGet("user_id").(int)
	role := c.MustGet("role").(models.Role)
	isAdmin := role == models.RoleAdmin

	if cat.UserID == nil {
		if !isAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "Global categories can only be deleted by an admin"})
			return
		}
	} else if *cat.UserID != userID && !isAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "You don't have right to manage other users' categories"})
		return
	}

	// Bütçe referansı kontrolü GO'DA yapılıyor, veritabanında değil.
	// SEBEP: AutoMigrate budget_categories.category_id için FK ÜRETMEZ (hiçbir
	// modelde foreignKey etiketi yok). FK yoksa silme başarılı olur ve bütçede
	// öksüz bir satır kalır — kullanıcının toplam limiti sessizce yanlış görünür.
	used, err := h.budgets.CountByCategory(id)
	if err != nil {
		respondInternalError(c, err)
		return
	}
	if used > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "This category is used by a budget and cannot be deleted"})
		return
	}

	if err := h.categories.Delete(id); err != nil {
		if errors.Is(err, repositories.ErrCategoryNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Category not Found"})
			return
		}
		if errors.Is(err, repositories.ErrCategoryInUse) {
			c.JSON(http.StatusConflict, gin.H{"error": "This category is used by existing transactions and cannot be deleted"})
			return
		}
		respondInternalError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Category deleted!"})
}
