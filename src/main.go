package main

import (
	"GoGinMoneyCopilot/ai"
	"GoGinMoneyCopilot/auth"
	"GoGinMoneyCopilot/chat"
	"GoGinMoneyCopilot/database"
	"GoGinMoneyCopilot/handlers"
	"GoGinMoneyCopilot/maintenance"
	"GoGinMoneyCopilot/middleware"
	"GoGinMoneyCopilot/models"
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

	// Redis opsiyonel: REDIS_ADDR boşsa devre dışı kalır ve denylist yalnızca
	// Postgres'ten okunur. Ama adres VERİLDİYSE ulaşılamıyor olması sessizce
	// geçilmemeli — yanlış yapılandırmayı trafik almadan önce gör.
	if err := database.InitRedis(); err != nil {
		log.Fatal(err)
	}

	accountRepo := repositories.NewAccountRepository(database.DB)
	userRepo := repositories.NewUserRepository(database.DB)
	categoryRepo := repositories.NewCategoryRepository(database.DB)
	transactionRepo := repositories.NewTransactionRepository(database.DB)
	budgetRepo := repositories.NewBudgetRepository(database.DB)
	tokenRepo := repositories.NewTokenRepository(database.DB)

	// Arka plan döngülerinin (sweeper'lar, denylist yeniden-ısıtma) paylaştığı
	// tek kapatma sinyali; graceful shutdown'da close(sweeperStop) ile durur.
	sweeperStop := make(chan struct{})

	// Redis varsa denylist'in TAM KOPYASI orada tutulur; her istekteki
	// "bu token iptal mi" sorusu Postgres yerine bellekten cevaplanır.
	//
	// Warm-up ÖNCE çalışır (henüz sarmalanmamış gorm repo'suyla), sonra
	// sarmalama yapılır. Warm-up başarısız olursa ölümcül değil: nöbetçi
	// yazılmadığı için okumalar Postgres'e düşer — yavaş ama doğru.
	if database.RDB != nil {
		warmCtx, warmCancel := context.WithTimeout(context.Background(), 10*time.Second)
		n, err := repositories.WarmUpDenylist(warmCtx, database.RDB, tokenRepo, time.Now())
		warmCancel()
		if err != nil {
			log.Println("denylist warm-up failed, reads will fall back to postgres:", err)
		} else {
			log.Printf("denylist warm-up: %d active revocation(s) loaded into redis", n)
		}

		// PERİYODİK TEKRAR: nöbetçi yalnızca AÇILIŞTA yazılıyordu. Redis
		// uygulama AYAKTAYKEN kendi başına restart olursa (bakım, OOM) veri
		// gider ama uygulama bunu asla öğrenmez; nöbetçi bir daha
		// yazılmadığı için okumalar SÜRESİZ olarak Postgres'e düşer. Bu
		// döngü düşme süresini birkaç dakikayla sınırlıyor.
		go repositories.StartWarmUpLoop(database.RDB, tokenRepo,
			repositories.DefaultWarmUpInterval, sweeperStop)

		tokenRepo = repositories.NewRedisTokenRepository(database.RDB, tokenRepo)
	}
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
	// authLimiter : IP başına, SABİT eşik — brute-force'u pahalı kılar,
	//               planla ilgisi yok (kimlik doğrulanmadan plan bilinmez).
	// chatLimiter : KULLANICI başına, PLANA GÖRE DEĞİŞEN eşik — /chat her
	//               istekte gerçek para harcıyor; free/pro farklı tavan alır.
	authPerMin := intEnv("AUTH_RATE_PER_MIN", 10)

	// chatPlanLimits: plan bulunamazsa ya da sorgu başarısız olursa
	// LimitByPlan en KISITLAYICI (free) değere düşer — bir DB arızasında
	// maliyeti serbest bırakmak yanlış taraf olurdu.
	chatPlanLimits := map[string]int{
		string(models.PlanFree): intEnv("CHAT_RATE_PER_MIN_FREE", 5),
		string(models.PlanPro):  intEnv("CHAT_RATE_PER_MIN_PRO", 30),
	}
	chatDefaultLimit := chatPlanLimits[string(models.PlanFree)]

	// Bellekteki sayaçlar HER ZAMAN kurulur: Redis varsa yedek, yoksa asıl.
	// chatMem'in kurucudaki perMinute'ü yalnızca ham Allow() çağrılırsa
	// kullanılır (chat rotası hiç çağırmaz, her zaman AllowWithLimit kullanır)
	// — o yüzden burada "en kısıtlayıcı" free limiti veriyoruz, anlamsız bir
	// sabit yerine.
	authMem := middleware.NewRateLimiter(authPerMin, 5)
	chatMem := middleware.NewRateLimiter(chatDefaultLimit, 5)
	go authMem.StartSweeper(sweeperStop)
	go chatMem.StartSweeper(sweeperStop)

	// Redis varsa sayaç konteynerlerin dışına taşınır; birden fazla kopya
	// çalıştığında limit gerçekten uygulanır. Ölçülen: Redis'siz 2 kopyada
	// limit ~2 katına çıkıyordu.
	var authLimiter middleware.Limiter = authMem
	var chatLimiter middleware.FullLimiter = chatMem
	if database.RDB != nil {
		authLimiter = middleware.NewRedisLimiter(database.RDB, "auth", authPerMin, authMem)
		chatLimiter = middleware.NewRedisLimiter(database.RDB, "chat", chatDefaultLimit, chatMem)
		log.Println("rate limiting: using redis (in-process counters kept as fallback)")
	}

	// chatPlanFn — HER /chat isteğinde planı TAZE okur (JWT'ye gömmüyoruz):
	// kullanıcı abone olur olmaz yeni limit hemen uygulanır, 15 dk'lık access
	// token ömrünü beklemez. Bedeli: bir kullanıcı sorgusu daha — chat isteği
	// zaten kategori/hesap için veritabanına gittiğinden kabul edilebilir.
	chatPlanFn := func(c *gin.Context) (string, error) {
		userID := c.MustGet("user_id").(int)
		u, err := userRepo.GetByID(c.Request.Context(), userID)
		if err != nil {
			return "", err
		}
		return string(u.Plan), nil
	}

	r := gin.New()

	// Rate limiting'in tamamı ClientIP()'nin doğruluğuna bağlı; gerekçe ve
	// vekil arkasına alındığında ne yapılacağı middleware/proxy.go'da.
	if err := middleware.SetupTrustedProxies(r); err != nil {
		log.Fatal(err)
	}

	r.Use(middleware.RequestLogger())
	r.Use(gin.Recovery())

	r.POST("/register", middleware.Limit(authLimiter, middleware.KeyByIP), authHandler.Register)	
	r.POST("/login", middleware.Limit(authLimiter, middleware.KeyByIP), authHandler.Login)

	// /auth/refresh KORUMASIZ olmalı: buraya zaten access token'ın süresi
	// dolduğu için geliyoruz. Kimlik doğrulaması refresh cookie'sinden gelir.
	r.POST("/auth/refresh", middleware.Limit(authLimiter, middleware.KeyByIP), authHandler.Refresh)

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
		authorized.POST("/chat",
			middleware.LimitByPlan(chatLimiter, middleware.KeyByUser, chatPlanFn, chatPlanLimits, chatDefaultLimit),
			chatHandler.Chat)
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
	// UseRedisLock(nil) zararsız: Redis devre dışıysa kilit atlanır, Cleaner
	// koşulsuz çalışır (RATIO: az olasılıklı israf, tek kopyada anlamsız).
	cleaner := maintenance.NewCleaner(tokenRepo, pendingRepo, refreshRepo, maintenance.DefaultInterval).
		UseRedisLock(database.RDB)
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
	if err := database.CloseRedis(); err != nil {
		log.Println("redis close:", err)
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
