package main

import (
	"GoGinMoneyCopilot/auth"
	"GoGinMoneyCopilot/database"
	"GoGinMoneyCopilot/models"
	"GoGinMoneyCopilot/middleware"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func main() {
	database.InitDB()
	r := gin.Default()


	r.POST("/register", func(c *gin.Context) {
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
	})

	r.POST("/login", func(c *gin.Context) {
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
	})

	r.POST("/accounts", middleware.AuthMiddleware(), func(c *gin.Context) {
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
	})

	r.GET("/accounts/:id", middleware.AuthMiddleware(), func(c *gin.Context) {
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
	})

	r.PUT("/accounts/:id", middleware.AuthMiddleware(), func(c *gin.Context) {
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

		c.JSON(http.StatusOK, gin.H{
			"message": "Account updated!",
		})
	})

	r.DELETE("/accounts/:id", middleware.AuthMiddleware(), func(c *gin.Context) {
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
	})

	r.POST("/categories", middleware.AuthMiddleware(), func(c *gin.Context) {
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
	})

	r.GET("/categories", middleware.AuthMiddleware(), func(c *gin.Context) {
		userID := c.MustGet("user_id").(int)

		categories, err := database.GetCategoriesForUser(userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, categories)
	})

	r.Run(":8080")
}

//Validation eklenecek daha detaylı yanlış değer tipleri, hatalı datalar (Dışına bakıyorsun ama içine değil "1997" senin için bir name mesela ?)
//Auth - Interceptor(Middleware) JSON web token için ilk giriş route'u ondan sonra CRUD'lara geçiş
//Routelar valueback atıyorlar otomatik c.JSON'la istersen kendin handlelayabilirsin onu bir değişkende saklayıp sonra eklersin response := c.JSON(...)

//Extension'ı ayarla standart fonksiyonları gezmeyi, açıklamalarını aç
//.env dosyası kurulcak bağlanılacak(DB connstr bilgileri olsun etc.)
