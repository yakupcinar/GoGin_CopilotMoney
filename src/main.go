package main

import (
	"GoGinMoneyCopilot/database"
	"GoGinMoneyCopilot/handlers"
	"GoGinMoneyCopilot/middleware"
	"GoGinMoneyCopilot/repositories"
	"GoGinMoneyCopilot/validators"
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}
	if os.Getenv("JWT_SECRET") == "" {
		log.Fatal("JWT_SECRET is not set")
	}
	validators.RegisterCustomValidators()

	if err := database.InitDB(); err != nil {
		log.Fatal(err)
	}

	accountRepo := repositories.NewAccountRepository(database.DB)
	userRepo := repositories.NewUserRepository(database.DB)
	categoryRepo := repositories.NewCategoryRepository(database.DB)
	transactionRepo := repositories.NewTransactionRepository(database.DB)
	tokenRepo := repositories.NewTokenRepository(database.DB)

	accountHandler := handlers.NewAccountHandler(accountRepo)
	categoryHandler := handlers.NewCategoryHandler(categoryRepo)
	transactionHandler := handlers.NewTransactionHandler(transactionRepo, accountRepo)
	authHandler := handlers.NewAuthHandler(userRepo, tokenRepo)

	r := gin.New()
	r.Use(middleware.RequestLogger())
	r.Use(gin.Recovery())

	r.POST("/register", authHandler.Register)
	r.POST("/login", authHandler.Login)

	authorized := r.Group("/")
	authorized.Use(middleware.AuthMiddleware(tokenRepo))
	{
		authorized.POST("/logout", authHandler.Logout)

		accounts := authorized.Group("/accounts")
		{
			accounts.POST("", accountHandler.CreateAccount)
			accounts.GET("/:id", accountHandler.GetAccount)
			accounts.PUT("/:id", accountHandler.UpdateAccount)
			accounts.DELETE("/:id", accountHandler.DeleteAccount)
			accounts.GET("/:id/transactions", transactionHandler.ListAccountTransactions)
		}

		categories := authorized.Group("/categories")
		{
			categories.POST("", categoryHandler.CreateCategory)
			categories.GET("", categoryHandler.ListCategories)
			categories.PUT("/:id", categoryHandler.UpdateCategory)
			categories.DELETE("/:id", categoryHandler.DeleteCategory)
		}

		transactions := authorized.Group("/transactions")
		{
			transactions.POST("", transactionHandler.CreateTransaction)
			transactions.GET("/:id", transactionHandler.GetTransaction)
			transactions.PUT("/:id", transactionHandler.UpdateTransaction)
			transactions.DELETE("/:id", transactionHandler.DeleteTransaction)
		}
	}

	srv := &http.Server{
		Addr:    ":8080",
		Handler: r,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	if sqlDB, err := database.DB.DB(); err == nil {
		sqlDB.Close()
	}

	log.Println("Server exited gracefully")
}
