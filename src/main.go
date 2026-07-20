package main

import (
	"GoGinMoneyCopilot/database"
	"GoGinMoneyCopilot/handlers"
	"GoGinMoneyCopilot/middleware"
	"log"
	"regexp"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"github.com/joho/godotenv"
)

func registerCustomValidators() { // To new file this func
	v, ok := binding.Validator.Engine().(*validator.Validate)
	if !ok {
		log.Fatal("Could not register custom validators")
	}

	accountNameRe := regexp.MustCompile(`^[\p{L}0-9 ]+$`)
	v.RegisterValidation("accountname", func(fl validator.FieldLevel) bool {
		return accountNameRe.MatchString(fl.Field().String())
	})
}

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}
	registerCustomValidators()
	database.InitDB()
	r := gin.New()
	r.Use(middleware.RequestLogger())
	r.Use(gin.Recovery())

	r.POST("/register", handlers.Register)
	r.POST("/login", handlers.Login)

	r.POST("/accounts", middleware.AuthMiddleware(), handlers.CreateAccount)
	r.GET("/accounts/:id", middleware.AuthMiddleware(), handlers.GetAccount)
	r.PUT("/accounts/:id", middleware.AuthMiddleware(), handlers.UpdateAccount)
	r.DELETE("/accounts/:id", middleware.AuthMiddleware(), handlers.DeleteAccount)

	r.POST("/categories", middleware.AuthMiddleware(), handlers.CreateCategory)
	r.GET("/categories", middleware.AuthMiddleware(), handlers.ListCategories)
	r.PUT("/categories/:id", middleware.AuthMiddleware(), handlers.UpdateCategory)
	r.DELETE("/categories/:id", middleware.AuthMiddleware(), handlers.DeleteCategory)

	r.POST("/transactions", middleware.AuthMiddleware(), handlers.CreateTransaction)
	r.GET("/transactions/:id", middleware.AuthMiddleware(), handlers.GetTransaction)
	r.GET("/accounts/:id/transactions", middleware.AuthMiddleware(), handlers.ListAccountTransactions)
	r.PUT("/transactions/:id", middleware.AuthMiddleware(), handlers.UpdateTransaction)
	r.DELETE("/transactions/:id", middleware.AuthMiddleware(), handlers.DeleteTransaction)

	r.Run(":8080")
}

