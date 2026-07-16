package handlers

import (
	"GoGinMoneyCopilot/auth"
	"GoGinMoneyCopilot/database"
	"GoGinMoneyCopilot/models"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

func Register(c *gin.Context) {
	var input models.RegisterInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input format!"})
		return
	}

	hashedPassword, err := auth.HashPassword(input.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Password couldn't made"})
		return
	}
	if err := database.CreateUser(input.Username, hashedPassword); err != nil {
		if errors.Is(err, database.ErrUsernameTaken) {
			c.JSON(http.StatusConflict, gin.H{"error": "Username Already Exist!"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "Register succesful!"})
}

func Login(c *gin.Context) {
	var input models.LoginInput
	if err := c.ShouldBind(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input format!"})
		return
	}

	user, err := database.GetUserByUsername(input.Username)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Username or Password is wrong!"})
		return
	}

	if !auth.CheckPassword(input.Password, user.PasswordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Username or Password is wrong!"})
		return
	}

	token, err := auth.GenerateToken(user.ID, user.IsAdmin)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Token couldn't be created"})
		return
	}

	c.JSON(http.StatusOK, models.LoginResponse{Token: token})
}
