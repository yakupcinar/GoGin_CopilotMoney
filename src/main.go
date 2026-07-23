package main

import (
	"GoGinMoneyCopilot/ai"
	"GoGinMoneyCopilot/auth"
	"GoGinMoneyCopilot/chat"
	"GoGinMoneyCopilot/database"
	"GoGinMoneyCopilot/handlers"
	"GoGinMoneyCopilot/maintenance"
	"GoGinMoneyCopilot/middleware"
	"GoGinMoneyCopilot/repositories"
	"GoGinMoneyCopilot/validators"
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}
	// JWT_SECRET yalnızca "var mı" değil "yeterince güçlü mü" diye de kontrol
	// edilir. 32 karakterin altı (HMAC-SHA256 için ~256 bit) bir anahtar ya da
	// geliştirmeden kalma zayıf bir placeholder üretime taşınırsa uygulama açılmaz.
	if len(os.Getenv("JWT_SECRET")) < 32 {
		log.Fatal("JWT_SECRET must be set and at least 32 characters (generate: openssl rand -base64 48)")
	}
	// Tehlikeli cookie kombinasyonlarını BAŞLANGIÇTA yakala.
	// SameSite=None + Secure=false olursa tarayıcı cookie'yi sessizce reddeder;
	// kullanıcı "giriş yapamıyorum" der, sebebi hiçbir logda görünmez.
	if err := auth.ValidateCookieConfig(); err != nil {
		log.Fatal(err)
	}
	validators.RegisterCustomValidators()

	if err := database.InitDB(); err != nil {
		log.Fatal(err)
	}

	accountRepo := repositories.NewAccountRepository(database.DB)
	userRepo := repositories.NewUserRepository(database.DB)
	categoryRepo := repositories.NewCategoryRepository(database.DB)
	transactionRepo := repositories.NewTransactionRepository(database.DB)
	budgetRepo := repositories.NewBudgetRepository(database.DB)
	tokenRepo := repositories.NewTokenRepository(database.DB)
	pendingRepo := repositories.NewPendingActionRepository(database.DB)
	refreshRepo := repositories.NewRefreshTokenRepository(database.DB)

	// --- AI / chat zinciri ---
	// GROQ_API_KEY yoksa chat özelliği KAPALI olur; uygulamanın geri kalanı
	// normal çalışır. chatService nil kalır, handler 503 döner.
	var chatService *chat.ActionService
	if parser, err := ai.NewGroqParser(); err != nil {
		log.Printf("Chat feature disabled: %v", err)
	} else {
		chatService = chat.NewActionService(
			parser, accountRepo, categoryRepo, transactionRepo, budgetRepo, pendingRepo)
		log.Println("Chat feature enabled")
	}

	accountHandler := handlers.NewAccountHandler(accountRepo)
	categoryHandler := handlers.NewCategoryHandler(categoryRepo, budgetRepo)
	transactionHandler := handlers.NewTransactionHandler(transactionRepo, accountRepo)
	budgetHandler := handlers.NewBudgetHandler(budgetRepo, categoryRepo, accountRepo, transactionRepo)
	authHandler := handlers.NewAuthHandler(userRepo, tokenRepo, refreshRepo)
	chatHandler := handlers.NewChatHandler(chatService)

	// --- Rate limiting ---
	// authLimiter : IP başına — brute-force'u pahalı kılar
	// chatLimiter : KULLANICI başına — /chat her istekte gerçek para harcıyor
	authLimiter := middleware.NewRateLimiter(intEnv("AUTH_RATE_PER_MIN", 10), 5)
	chatLimiter := middleware.NewRateLimiter(intEnv("CHAT_RATE_PER_MIN", 20), 5)
	sweeperStop := make(chan struct{})
	go authLimiter.StartSweeper(sweeperStop)
	go chatLimiter.StartSweeper(sweeperStop)

	r := gin.New()
	r.Use(middleware.RequestLogger())
	r.Use(gin.Recovery())

	r.POST("/register", authLimiter.Limit(middleware.KeyByIP), authHandler.Register)
	r.POST("/login", authLimiter.Limit(middleware.KeyByIP), authHandler.Login)

	// /auth/refresh KORUMASIZ olmalı: buraya zaten access token'ın süresi
	// dolduğu için geliyoruz. Kimlik doğrulaması refresh cookie'sinden gelir.
	r.POST("/auth/refresh", authLimiter.Limit(middleware.KeyByIP), authHandler.Refresh)

	authorized := r.Group("/")
	authorized.Use(middleware.AuthMiddleware(tokenRepo))
	{
		// Logout /auth altında: refresh cookie'nin Path'i /auth olduğu için
		// cookie ancak buraya gönderilir — token'ı DB'den iptal edebilmek
		// için değerini görmemiz gerekiyor.
		authorized.POST("/auth/logout", authHandler.Logout)

		// Chat: serbest metinden eylem üretir. Yıkıcı işlemler token'lı
		// onay gerektirir; frontend "Emin misiniz?" popup'ında summary'yi
		// gösterip token'ı /actions/confirm'e gönderir.
		authorized.POST("/chat", chatLimiter.Limit(middleware.KeyByUser), chatHandler.Chat)
		authorized.POST("/actions/confirm", chatHandler.Confirm)

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

		budgets := authorized.Group("/budgets")
		{
			budgets.POST("", budgetHandler.CreateBudget)
			budgets.GET("", budgetHandler.GetBudget)
			budgets.PUT("", budgetHandler.UpdateBudget)
			budgets.DELETE("", budgetHandler.DeleteBudget)
		}
	}

	// Periyodik bakım: süresi geçmiş kayıtları temizler.
	// Üç tablo da (revoked_tokens, pending_actions, refresh_tokens) her
	// kullanımda satır biriktiriyor ve hiçbiri kendini temizlemiyordu.
	cleanupCtx, stopCleanup := context.WithCancel(context.Background())
	cleaner := maintenance.NewCleaner(tokenRepo, pendingRepo, refreshRepo, maintenance.DefaultInterval)
	go cleaner.Start(cleanupCtx)

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

	// Bakım işçisini ve rate-limit temizleyicilerini durdur.
	stopCleanup()
	close(sweeperStop)

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

// intEnv — pozitif tamsayı ortam değişkeni, yoksa/geçersizse varsayılan.
func intEnv(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		log.Printf("%s is invalid (%q), using default: %d", key, v, fallback)
		return fallback
	}
	return n
}
