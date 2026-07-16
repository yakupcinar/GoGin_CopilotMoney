package handlers

import (
	"GoGinMoneyCopilot/database"
	"GoGinMoneyCopilot/models"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func CreateCategory(c *gin.Context) {
	var input models.CreateCategoryInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input format!"})
		return
	}

	userID := c.MustGet("user_id").(int)

	if err := database.CreateCategory(input.Name, input.Type, &userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"message": "Category created!",
		"name":    input.Name,
	})
}

func ListCategories(c *gin.Context) {
	userID := c.MustGet("user_id").(int)

	categories, err := database.GetCategoriesForUser(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, categories)
}

func UpdateCategory(c *gin.Context) {
	idParam := c.Param("id")
	id, err := strconv.Atoi(idParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID Format"})
		return
	}

	cat, err := database.GetCategory(id)
	if err != nil {
		if errors.Is(err, database.ErrCategoryNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Category not Found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	userID := c.MustGet("user_id").(int)
	isAdmin := c.MustGet("is_admin").(bool)

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

	if err := database.UpdateCategory(id, input.Name, input.Type); err != nil {
		if errors.Is(err, database.ErrCategoryNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Category not Found!"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Category updated!"})
}

func DeleteCategory(c *gin.Context) {
	idParam := c.Param("id")
	id, err := strconv.Atoi(idParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID Format"})
		return
	}

	cat, err := database.GetCategory(id)
	if err != nil {
		if errors.Is(err, database.ErrCategoryNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Category not Found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	userID := c.MustGet("user_id").(int)
	isAdmin := c.MustGet("is_admin").(bool)

	if cat.UserID == nil {
		if !isAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "Global categories can only be deleted by an admin"})
			return
		}
	} else if *cat.UserID != userID && !isAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "You don't have right to manage other users' categories"})
		return
	}

	if err := database.DeleteCategory(id); err != nil {
		if errors.Is(err, database.ErrCategoryNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Category not Found"})
			return
		}
		if errors.Is(err, database.ErrCategoryInUse) {
			c.JSON(http.StatusConflict, gin.H{"error": "This category is used by existing transactions and cannot be deleted"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Category deleted!"})
}
