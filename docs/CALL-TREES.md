# Çağrı Ağaçları

`FLOWS.md`'deki akışların **kompakt gösterimi**. Aynı içerik, farklı biçim.

| dosya | biçim | ne zaman |
|---|---|---|
| `FLOWS.md` | kutulu diyagram + numaralı açıklamalar | anlatırken, sunarken |
| `CALL-TREES.md` | girintili çağrı ağacı | kod okurken, hızlı bakarken, `grep`'lerken |

**Gösterim:** girinti çağrı derinliğini, `├─ └─` aynı seviyedeki adımları
gösterir. `──►` akışı kesen çıkışlar, `[...]` okta akan veri, `[I/O]` süreç
dışına çıkan çağrı.

Gerekçeler burada **yok** — her adımın neden öyle olduğu `FLOWS.md`'deki
numaralı notlarda. Bu dosya "ne oluyor" sorusuna, o dosya "neden" sorusuna
cevap verir.

## İçindekiler

- [1 — Kurulum ve kapanış](#1--kurulum-ve-kapanış)
- [2 — POST /login](#2--post-login)
- [3 — POST /transactions](#3--post-transactions)
- [4 — POST /auth/refresh](#4--post-authrefresh)
- [5 — POST /auth/logout](#5--post-authlogout)
- [6 — POST /chat](#6--post-chat)
- [7 — POST /actions/confirm](#7--post-actionsconfirm)
- [8 — Arka plan işleri](#8--arka-plan-işleri)

---

## 1 — Kurulum ve kapanış

```
main()
 │
 ├─ AÇILIŞ MUHAFIZLARI
 │    ├─ godotenv.Load()                       hata → sadece log
 │    ├─ len(JWT_SECRET) < 32 ? ─────────────► log.Fatal
 │    ├─ auth.ValidateCookieConfig() ────────► log.Fatal
 │    │    └─ SameSite=None && !Secure ? → errSameSiteNoneRequiresSecure
 │    └─ validators.RegisterCustomValidators()
 │         └─ binding.Validator.Engine().(*validator.Validate)
 │              └─ RegisterValidation("accountname", ^[\p{L}0-9 ]+$)
 │
 ├─ BAĞLANTILAR
 │    ├─ database.InitDB() ──────────────────► log.Fatal              [I/O]
 │    │    ├─ DSN := env(DB_HOST/PORT/USER/PASSWORD/NAME)
 │    │    ├─ for i := 0..9 { gorm.Open ; başarısızsa 2sn bekle }
 │    │    ├─ dbLogger()  IgnoreRecordNotFoundError=true
 │    │    └─ AutoMigrate(9 model) → database.DB
 │    └─ database.InitRedis() ───────────────► log.Fatal              [I/O]
 │         ├─ REDIS_ADDR == "" → nil döner (özellik kapalı)
 │         ├─ redis.NewClient(...)     ← hiçbir şey BAĞLAMAZ
 │         └─ client.Ping(ctx 5sn)     ← gerçek bağlantı testi
 │
 ├─ REPOSITORY'LER × 8
 │    └─ New*Repository(database.DB) → ARAYÜZ döner
 │
 ├─ sweeperStop := make(chan struct{})
 │
 ├─ DENYLIST ZİNCİRİ   (RDB != nil ise)
 │    ├─ WarmUpDenylist(ctx 10sn, RDB, tokenRepo, now)   SENKRON      [I/O]
 │    │    ├─ source.ListActive(now)   WHERE expires_at > now
 │    │    ├─ pipeline: N × Set("jti:<jti>", 1, ttl)
 │    │    ├─ pipeline: Set("denylist:warm", 1, TTL YOK)   ← EN SON
 │    │    └─ pipe.Exec()              hata → ölümcül DEĞİL
 │    ├─ go StartWarmUpLoop(RDB, tokenRepo, 5dk, sweeperStop)
 │    └─ tokenRepo = NewRedisTokenRepository(RDB, tokenRepo)   decorator
 │
 ├─ AI ZİNCİRİ
 │    ├─ ai.NewGroqParser()
 │    │    ├─ GROQ_API_KEY == "" → hata → chatService nil KALIR
 │    │    ├─ GROQ_MODEL   ?? "llama-3.3-70b-versatile"
 │    │    ├─ GROQ_BASE_URL ?? "https://api.groq.com/openai/v1"
 │    │    └─ &groqParser{apiKey, model, baseURL, http.Client{}}
 │    └─ chat.NewActionService(parser, accounts, categories, txs, budgets, pending)
 │
 ├─ HANDLER'LAR × 6
 │    └─ NewChatHandler(chatService)     ← nil olabilir → handler 503
 │
 ├─ RATE LIMITER'LAR
 │    ├─ authPerMin := intEnv("AUTH_RATE_PER_MIN", 10)
 │    ├─ chatPlanLimits{free: intEnv(…,5), pro: intEnv(…,30)}
 │    ├─ chatDefaultLimit := chatPlanLimits[free]     ← EN KISITLAYICI
 │    ├─ authMem := NewRateLimiter(authPerMin, burst=5)
 │    ├─ chatMem := NewRateLimiter(chatDefaultLimit, burst=5)
 │    ├─ go authMem.StartSweeper(sweeperStop)
 │    ├─ go chatMem.StartSweeper(sweeperStop)
 │    ├─ var authLimiter middleware.Limiter     = authMem   ← DAR
 │    ├─ var chatLimiter middleware.FullLimiter = chatMem   ← GENİŞ
 │    └─ RDB != nil ? NewRedisLimiter(RDB, ns, perMin, fallback=mem)
 │
 ├─ chatPlanFn := func(c) { c.MustGet("user_id") ; userRepo.GetByID }
 │
 ├─ MOTOR
 │    ├─ gin.New()                       ← Default() DEĞİL
 │    ├─ middleware.SetupTrustedProxies(r) ──► log.Fatal
 │    │    └─ TRUSTED_PROXIES == "" ? SetTrustedProxies(nil)
 │    ├─ r.Use(middleware.RequestLogger())
 │    ├─ r.Use(gin.Recovery())
 │    ├─ public × 3    /register /login /auth/refresh  + Limit(authLimiter, KeyByIP)
 │    └─ authorized := r.Group("/") ; Use(AuthMiddleware(tokenRepo))
 │         ├─ /auth/logout  /actions/confirm
 │         ├─ /chat  + LimitByPlan(chatLimiter, KeyByUser, chatPlanFn, …)
 │         └─ /accounts/*  /categories/*  /transactions/*  /budgets/*
 │
 ├─ BAKIM İŞÇİSİ
 │    ├─ cleanupCtx, stopCleanup := context.WithCancel(Background)
 │    ├─ NewCleaner(tokenRepo, pendingRepo, refreshRepo, 1saat)
 │    │    └─ .UseRedisLock(database.RDB)      ← nil zararsız
 │    └─ go cleaner.Start(cleanupCtx)
 │
 ├─ go srv.ListenAndServe()   :8080
 │
 ├─ quit := make(chan os.Signal, 1)
 ├─ signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
 ├─ <-quit                                    ← BURADA BEKLER
 │
 └─ KAPANIŞ
      ├─ stopCleanup()                → cleaner ctx.Done()
      ├─ close(sweeperStop)           → 3 goroutine birden
      ├─ srv.Shutdown(ctx 5sn)        → açık istekleri bitir
      ├─ database.DB.DB().Close()
      ├─ database.CloseRedis()
      └─ "Server exited gracefully"
```

---

## 2 — POST /login

```
POST /login   { username, password }
  │
  ├─ RequestLogger
  ├─ gin.Recovery
  ├─ Limit(authLimiter, KeyByIP)                              [I/O redis]
  │    ├─ KeyByIP(c) → "ip:" + c.ClientIP()
  │    └─ redisLimiter.Allow → incrementAndCheck
  │         ├─ ctx 200ms
  │         ├─ window := now.Unix() / 60
  │         ├─ pipeline: INCR rl:auth:ip:X:<w> ; EXPIRE 70s
  │         ├─ hata → warnOnce + fallback.Allow (bellek)
  │         └─ incr.Val() <= perMinute ?
  │              └─ hayır ──► tooManyRequests → 429 + Retry-After
  │
  └─ authHandler.Login
       ├─ ShouldBindJSON(&input) ──────────────► 400
       │     ▼ [models.LoginInput]
       ├─ users.GetByUsername(ctx, username)                  [I/O postgres]
       │    ├─ SELECT * FROM users WHERE username=? LIMIT 1
       │    ├─ gorm.ErrRecordNotFound → ErrUserNotFound
       │    └─ diğer hata → respondInternalError ──► 500
       │
       ├─ KULLANICI YOK dalı
       │    ├─ auth.CheckPassword(pw, dummyHash)      ← boşa bcrypt
       │    │    └─ bcrypt.CompareHashAndPassword     saf CPU
       │    └─ ──► 401 "Username or Password is wrong!"
       │
       ├─ KULLANICI VAR dalı              ▼ [*models.User]
       │    └─ auth.CheckPassword(pw, user.PasswordHash)
       │         └─ false ──► 401  (AYNI mesaj, AYNI süre)
       │
       └─ issueTokenPair(c, user)                    /auth/refresh ile ORTAK
            ├─ auth.GenerateToken(user.ID, user.Role) ──► 500
            │    ├─ generateJTI() → 16B crypto/rand → hex
            │    ├─ jwt.MapClaims{user_id, role, jti, exp}
            │    ├─ AccessTokenTTL() ← durationEnv("ACCESS_TOKEN_TTL", 15dk)
            │    └─ SignedString(jwtSecret())   HS256
            │         ▼ [access token]
            ├─ auth.NewRefreshToken() ──► 500
            │    ├─ 32B crypto/rand → base64   = raw
            │    └─ sha256(raw) → hex          = hash
            │         ▼ [raw, hash]
            ├─ refresh.Create({UserID, TokenHash: hash, ExpiresAt}) ──► 500  [I/O]
            ├─ auth.SetRefreshCookie(c, raw)
            │    ├─ c.SetSameSite(cookieSameSite())   ← ÖNCE
            │    └─ c.SetCookie("refresh_token", raw, ttl, "/auth",
            │                    domain, secure, httpOnly=true)
            └─ 200 { "token": … }
```

---

## 3 — POST /transactions

```
POST /transactions   { account_id, category_id, amount, type, description,
                       transaction_date }
  │
  ├─ RequestLogger
  ├─ gin.Recovery
  ├─ AuthMiddleware(tokenRepo)
  │    ├─ c.GetHeader("Authorization")     boş ──► 401
  │    ├─ strings.Split(" ") ; parts[0]=="Bearer" ?  ──► 401
  │    ├─ auth.ValidateToken(parts[1]) ──► 401         saf CPU
  │    │    ├─ jwt.Parse(…, WithValidMethods{"HS256"})
  │    │    ├─ claims["user_id"].(float64)   ← JSON sayıları float64
  │    │    ├─ claims["jti"].(string)
  │    │    ├─ claims["exp"].(float64)
  │    │    └─ claims["role"].(string)       ← hata YOK SAYILIR
  │    ├─ tokens.IsRevoked(ctx, claims.JTI)              [I/O redis→pg]
  │    │    ├─ redisTokenRepository.IsRevoked
  │    │    │    ├─ MGet("denylist:warm", "jti:<jti>")    TEK tur
  │    │    │    ├─ err || len!=2 || vals[0]==nil → source'a düş
  │    │    │    └─ return vals[1] != nil
  │    │    └─ gormTokenRepository.IsRevoked
  │    │         └─ COUNT WHERE jti=?   (jti = birincil anahtar)
  │    │    ├─ hata ──► 500      ← fail-closed
  │    │    └─ revoked ──► 401
  │    └─ c.Set: user_id · role · jti · token_exp
  │
  │  ⚠ rate limiter YOK
  │
  └─ transactionHandler.CreateTransaction
       ├─ ShouldBindJSON(&input) ──────────────► 400
       │     binding: account_id/category_id/amount(gt=0)/type(oneof)/
       │              description(max=100)/transaction_date
       │     ▼ [models.CreateTransactionInput]
       ├─ getAccountForRequest(c, h.accounts, input.AccountID)   [I/O postgres]
       │    ├─ role := c.MustGet("role").(models.Role)
       │    ├─ admin  → accounts.GetByID(accountID)
       │    ├─ değil  → accounts.GetByIDForUser(accountID, userID)   SAHİPLİK
       │    ├─ ErrAccountNotFound ──► 404      (403 DEĞİL)
       │    └─ diğer ──► 500
       │
       │  ⚠⚠ category_id için SAHİPLİK KONTROLÜ YOK          ← BULGU
       │
       ├─ transactions.Create(ctx, input)                     [I/O postgres]
       │    ├─ input → models.Transaction
       │    ├─ db.Create(&tx)
       │    │    ├─ FK    account_id → accounts
       │    │    ├─ FK    category_id → categories   (varlık ✓ sahiplik ✗)
       │    │    └─ CHECK type IN ('income','expense')
       │    └─ hata ──► 500
       └─ 201 { "message": "Transaction created!" }     ← id DÖNMÜYOR
```

---

## 4 — POST /auth/refresh

```
POST /auth/refresh    gövde YOK · Cookie: refresh_token=<raw>
  │
  ├─ RequestLogger
  ├─ gin.Recovery
  ├─ Limit(authLimiter, KeyByIP) ──────────────► 429           [I/O redis]
  │
  │  ⚠ AuthMiddleware YOK — access token'ın süresi dolduğu için gelindi
  │
  └─ authHandler.Refresh
       ├─ auth.RefreshTokenFromRequest(c)
       │    └─ c.Cookie("refresh_token")   hata → "" ──► 401
       │         ▼ [raw]
       ├─ auth.HashRefreshToken(raw)      sha256 → hex
       │         ▼ [hash]
       ├─ refresh.Consume(ctx, hash, now)                      [I/O postgres]
       │    ├─ UPDATE refresh_tokens SET used_at = now
       │    │   WHERE token_hash=? AND used_at IS NULL
       │    │         AND revoked_at IS NULL AND expires_at > ?
       │    ├─ RowsAffected == 1
       │    │    └─ SELECT … WHERE token_hash=?     ▼ [*RefreshToken]
       │    └─ RowsAffected == 0 → TEŞHİS için ikinci sorgu
       │         ├─ kayıt hiç yok        → ErrRefreshTokenInvalid  record=nil
       │         ├─ existing.UsedAt != nil → ErrRefreshTokenReused record=DOLU
       │         └─ iptal/süresi dolmuş  → ErrRefreshTokenInvalid  record=nil
       │
       ├─ hata dalı → handleRefreshFailure(c, record, err, now)
       │    ├─ Reused && record != nil ?
       │    │    └─ refresh.RevokeAllForUser(record.UserID, now)  [I/O postgres]
       │    │         UPDATE … SET revoked_at=now WHERE user_id=? AND revoked_at IS NULL
       │    │         log "SECURITY: refresh token reused"
       │    ├─ !Invalid ise → log (beklenmeyen altyapı hatası)
       │    ├─ auth.ClearRefreshCookie(c)
       │    └─ ──► 401 "Session expired, please log in again"   HER DURUMDA AYNI
       │
       ├─ users.GetByID(ctx, record.UserID)      ROLÜ TAZE OKU  [I/O postgres]
       │    └─ hata → ClearRefreshCookie ──► 401
       │         ▼ [*models.User]
       │
       └─ issueTokenPair(c, user)                    /login ile ORTAK
            ├─ auth.GenerateToken(...)   → YENİ jti
            ├─ auth.NewRefreshToken()    → yeni raw + hash
            ├─ refresh.Create(hash)                             [I/O postgres]
            ├─ auth.SetRefreshCookie(c, raw)
            └─ 200 { "token": … }
```

---

## 5 — POST /auth/logout

```
POST /auth/logout   gövde YOK
                    Authorization: Bearer <access> · Cookie: refresh_token=<raw>
  │
  ├─ RequestLogger
  ├─ gin.Recovery
  ├─ AuthMiddleware(tokenRepo)  ──► 401 · 500                   [I/O]
  │    └─ c.Set: user_id · role · jti · token_exp
  │
  └─ authHandler.Logout
       ├─ jti := c.MustGet("jti").(string)
       ├─ exp := c.MustGet("token_exp").(time.Time)
       ├─ now := time.Now()
       │
       ├─ [A] tokens.Revoke(ctx, jti, exp)          ACCESS TOKEN
       │    └─ redisTokenRepository.Revoke
       │         ├─ source.Revoke(ctx, jti, exp)                [I/O postgres]
       │         │    ├─ INSERT revoked_tokens{JTI, ExpiresAt}
       │         │    ├─ clause.OnConflict{DoNothing: true}     idempotent
       │         │    └─ hata ──► 500  ve  AKIŞ DURUR
       │         ├─ ttl := time.Until(exp) ; ttl <= 0 → Redis'e yazma
       │         └─ rdb.Set("jti:<jti>", 1, ttl)                [I/O redis]
       │              └─ hata → rdb.Del("denylist:warm") + log
       │                        hata DÖNDÜRME
       │
       ├─ [B] raw := auth.RefreshTokenFromRequest(c)   REFRESH TOKEN
       │    ├─ raw == "" → BU ADIM ATLANIR (hata değil)
       │    └─ refresh.Revoke(ctx, HashRefreshToken(raw), now)  [I/O postgres]
       │         ├─ UPDATE refresh_tokens SET revoked_at=now
       │         │   WHERE token_hash=? AND revoked_at IS NULL   idempotent
       │         └─ hata → SADECE LOG, akış devam eder
       │
       ├─ [C] auth.ClearRefreshCookie(c)               COOKIE
       │    ├─ c.SetSameSite(cookieSameSite())
       │    └─ c.SetCookie(name, "", -1, "/auth", domain, secure, true)
       │         nitelikler BİREBİR aynı olmalı
       │
       └─ 200 { "message": "Logged out" }
```

---

## 6 — POST /chat

```
POST /chat   { text, account_id }
  │
  ├─ RequestLogger
  ├─ gin.Recovery
  ├─ AuthMiddleware(tokenRepo) ──► 401 · 500                    [I/O]
  │    └─ c.Set: user_id · role · jti · token_exp
  │
  ├─ LimitByPlan(chatLimiter, KeyByUser, chatPlanFn, limits, default)
  │    ├─ chatPlanFn(c)                                         [I/O postgres]
  │    │    ├─ userID := c.MustGet("user_id").(int)
  │    │    ├─ userRepo.GetByID(ctx, userID)     ← plan HER İSTEKTE taze
  │    │    └─ hata → limit = defaultLimit (free)
  │    ├─ limit := limits[plan]     free:5 / pro:30
  │    ├─ KeyByUser(c) → "user:<id>"   (kimlik yoksa "ip:…")
  │    └─ chatLimiter.AllowWithLimit(ctx, key, limit)           [I/O redis]
  │         ├─ incrementAndCheck(ctx, key, limit)
  │         │    ├─ window := now.Unix()/60
  │         │    ├─ pipeline: INCR rl:chat:user:<id>:<w> ; EXPIRE 70s
  │         │    └─ incr.Val() <= limit ?
  │         ├─ hata → fallback.(PlanAwareLimiter).AllowWithLimit
  │         └─ false ──► tooManyRequests → 429
  │
  └─ chatHandler.Chat
       ├─ h.service == nil ? ──────────────────► 503
       ├─ ShouldBindJSON(&body)  text required,max=500 ──► 400
       ├─ userID := c.MustGet("user_id") ; role := c.MustGet("role")
       │     ▼ [chat.ChatRequest{UserID, Role, DefaultAccountID, Text}]
       │
       └─ ActionService.Chat(ctx, req)
            ├─ len([]rune(req.Text)) == 0 ?  ──► ErrEmptyText
            ├─ len([]rune(req.Text)) > 500 ? ──► ErrTextTooLong
            │     ↑ AI'DAN ÖNCE
            ├─ today := startOfDay(time.Now())
            ├─ categories.GetForUser(ctx, userID)               [I/O postgres]
            ├─ accounts.ListForUser(ctx, userID)                [I/O postgres]
            │
            ├─ parser.Parse(ctx, ai.ParseInput{Text, Categories, Accounts, Today})
            │    └─ groqParser.Parse                            [I/O groq]
            │         └─ parseOnce(ctx, in)
            │              ├─ systemPrompt()
            │              │    ├─ systemPromptBase
            │              │    ├─ models.AllowedIntents()
            │              │    └─ outputSchema() → actionSchema() → compactSchema()
            │              ├─ buildUserPrompt(in)
            │              │    ├─ kullanıcının kategorileri + hesapları
            │              │    └─ weekdayTR(today)
            │              ├─ chatRequest{model, messages[system,user]}
            │              ├─ http.Client.Do(POST baseURL/chat/completions)
            │              ├─ 429 → retryAfterFrom(resp, body) → bekle, tekrar
            │              ├─ stripCodeFence(content)
            │              ├─ json.Unmarshal → []models.ParsedAction
            │              └─ hata → truncateForError → "parsing failed: %w"
            │                   ▼ [[]models.ParsedAction]   ← N tane
            │
            └─ for i := range actions:  handle(ctx, &actions[i], req, categories, today)
                 ├─ models.RiskOf(a.Intent)              BEYAZ LİSTE
                 │    └─ !ok → Result{Error: "unknown or not-allowed action"}
                 │              payload YOK · token YOK · DÖN
                 │
                 ├─ OKUMA  (RiskRead)
                 │    ├─ list_categories   → res.Data = categories
                 │    ├─ get_account       → resolveAccount        SAHİPLİK
                 │    ├─ list_transactions → resolveAccount + txs.ListByAccount
                 │    ├─ get_transaction   → resolveTransaction    SAHİPLİK
                 │    └─ budget_view
                 │         ├─ |offset| > MaxPeriodOffset ? → "period range too large"
                 │         └─ BuildBudgetView(budgets, categories, accounts, txs, …)
                 │              ├─ Budget.PeriodAt(today, offset)
                 │              ├─ txs.SumExpenseByCategory(accountIDs, from, to)
                 │              └─ round2(...)
                 │         ▼ res.Data
                 │
                 ├─ OLUŞTURMA  (RiskCreate)          TASLAK üret, YAZMA
                 │    ├─ create_account   → Name boş? NeedsInput
                 │    ├─ create_category  → Name/Type boş? NeedsInput
                 │    │                     findCategoryByName → Warning
                 │    ├─ create_transaction → buildTransaction
                 │    │    ├─ resolveAccount / resolveCategory     SAHİPLİK
                 │    │    │    ├─ matchAccount / matchCategory  (isim eşleştirme)
                 │    │    │    ├─ cat.UserID == nil → ErrGlobalCategory
                 │    │    │    └─ *cat.UserID != userID → ErrCategoryNotFound
                 │    │    ├─ Amount == 0 ? → res.Error (REDDET)
                 │    │    ├─ kategori çözülemedi → NeedsInput + Warning
                 │    │    └─ dateInWindow(d, today) ? değilse → düzelt + Warning
                 │    └─ budget_set → buildBudget
                 │         ▼ res.Payload
                 │
                 └─ YIKICI  (RiskDestructive)        TOKEN üret, YAPMA
                      ├─ prepareCategoryAction / prepareAccountAction
                      │  prepareTransactionAction / prepareBudgetAction
                      │  prepareBudgetUpdateAction
                      │    ├─ resolve*                            SAHİPLİK
                      │    ├─ yapılabilir mi? (kullanımda mı, global mi)
                      │    │    └─ hayır → res.Error · TOKEN YOK
                      │    ├─ verbOf(intent) → özet metni
                      │    └─ attachConfirmation(ctx, &res, userID, intent,
                      │                          targetID, summary, params)
                      │         ├─ newToken() → "act_…"
                      │         ├─ pending.Create(&PendingAction{…})   [I/O postgres]
                      │         │    ExpiresAt = now + pendingTTL
                      │         └─ res.RequiresConfirmation = true
                      │            res.Token · res.Summary
                      ▼ [Result]
            ▼ [[]Result]
       │
       ├─ err != nil → respondChatError
       │    ├─ ErrEmptyText / ErrTextTooLong ──► 400 (sebebiyle)
       │    └─ diğer → log ──► 503
       └─ 200 { "results": [...] }
            handle() içi hatalar res.Error'a yazılır, kod 200 KALIR
```

---

## 7 — POST /actions/confirm

```
POST /actions/confirm   { token }
  │
  ├─ RequestLogger
  ├─ gin.Recovery
  ├─ AuthMiddleware(tokenRepo) ──► 401 · 500                    [I/O]
  │
  │  ⚠ rate limiter YOK
  │
  └─ chatHandler.Confirm
       ├─ h.service == nil ? ──────────────────► 503
       ├─ ShouldBindJSON(&body)  token required,max=64 ──► 400
       ├─ userID := c.MustGet("user_id").(int)
       │
       └─ ActionService.Confirm(ctx, userID, token)
            ├─ pending.Claim(ctx, userID, token, now)           [I/O postgres]
            │    ├─ UPDATE pending_actions SET used_at = now
            │    │   WHERE token=? AND user_id=?
            │    │         AND used_at IS NULL AND expires_at > ?
            │    ├─ RowsAffected == 0 → ErrPendingActionInvalid
            │    │    (yok / başkasının / kullanılmış / süresi dolmuş — AYNI)
            │    └─ SELECT … WHERE token=?      ▼ [*models.PendingAction]
            │
            └─ switch action.Intent      8 yıkıcı niyet
                 ├─ IntentDeleteCategory   → confirmDeleteCategory
                 │    ├─ ownedCategory(ctx, userID, a.TargetID)  [I/O postgres]
                 │    │    ├─ categories.GetByID → ErrCategoryNotFound ──► 404
                 │    │    ├─ cat.UserID == nil  → ErrGlobalCategory   ──► 403
                 │    │    └─ *cat.UserID != userID → ErrCategoryNotFound ──► 404
                 │    ├─ txs.CountByCategory(cat.ID)   TOCTOU      [I/O postgres]
                 │    │    └─ used > 0 → ErrCategoryInUse          ──► 409
                 │    ├─ categories.Delete(cat.ID)     YAZMA       [I/O postgres]
                 │    └─ "category %q deleted"
                 ├─ IntentUpdateCategory    → confirmUpdateCategory
                 ├─ IntentDeleteAccount     → confirmDeleteAccount
                 ├─ IntentUpdateAccount     → confirmUpdateAccount
                 ├─ IntentDeleteTransaction → confirmDeleteTransaction
                 ├─ IntentUpdateTransaction → confirmUpdateTransaction
                 ├─ IntentBudgetDelete      → confirmDeleteBudget
                 ├─ IntentBudgetUpdate      → confirmUpdateBudget
                 └─ default → "this action cannot be run via confirmation"
       │
       ├─ err != nil → respondConfirmError
       │    ├─ ErrPendingActionInvalid ──────────► 400
       │    ├─ ErrCategoryInUse / ErrAccountInUse ► 409
       │    ├─ *NotFound (category/account/transaction/budget) ► 404
       │    ├─ chat.ErrGlobalCategory ───────────► 403
       │    ├─ *chat.ValidationError ────────────► 400 (ve.Msg ile)
       │    └─ default → respondInternalError ───► 500
       └─ 200 { "message": … }
```

---

## 8 — Arka plan işleri

```
Dört goroutine, ortak iskelet:
    ticker := time.NewTicker(interval) ; defer ticker.Stop()
    for { select { case <-stop: return ; case <-ticker.C: iş } }

├─ StartWarmUpLoop(RDB, tokenRepo, 5dk, sweeperStop)   PAYLAŞILAN DURUM
│    └─ her tur:
│         ├─ ctx := WithTimeout(Background, 10sn)   ← YENİ, defer YOK
│         ├─ runWarmUpIfLeader(ctx, rdb, source, interval, now)
│         │    ├─ rdb.SetNX("denylist:warmup:lock", 1, ttl=5dk)   [I/O redis]
│         │    │    ├─ hata      → log + turu atla
│         │    │    └─ !acquired → SESSİZCE atla
│         │    └─ WarmUpDenylist(ctx, rdb, source, now)
│         │         ├─ source.ListActive(now)  WHERE expires_at>now  [I/O pg]
│         │         ├─ pipeline: N × Set("jti:<jti>", 1, ttl)        [I/O redis]
│         │         ├─ pipeline: Set("denylist:warm", 1, TTL YOK) ← EN SON
│         │         └─ pipe.Exec()
│         └─ cancel()
│    durdurma: <-sweeperStop
│    ⚠ kilit iş bitince BIRAKILMAZ — TTL ile düşer (turda-bir-kez garantisi)
│
├─ cleaner.Start(cleanupCtx)                          PAYLAŞILAN DURUM
│    ├─ İLK TUR HEMEN çalışır
│    └─ her saat: runIfLeader(ctx, now)
│         ├─ c.rdb == nil ? → RunOnce koşulsuz
│         ├─ rdb.SetNX("cleaner:lock", 1, ttl=1saat)               [I/O redis]
│         │    ├─ hata → log + atla
│         │    └─ !acquired → atla
│         └─ RunOnce(ctx, now)                                     [I/O pg]
│              ├─ tokens.DeleteExpired(now)   ─┐
│              ├─ pending.DeleteExpired(now)   ├ biri hata verse DİĞERLERİ DEVAM
│              ├─ refresh.DeleteExpired(now)  ─┘
│              └─ logRun(rep)  → rep.Total()==0 ise LOGLAMA
│    durdurma: <-cleanupCtx.Done()
│
├─ authMem.StartSweeper(sweeperStop)                  SÜREÇ-YEREL
│    └─ her 10dk: Sweep(now)
│         └─ visitors map: now.Sub(v.lastSeen) > ttl → delete
│    AĞ YOK · KİLİT YOK · her kopya KENDİ haritasını temizler
│
└─ chatMem.StartSweeper(sweeperStop)                  SÜREÇ-YEREL
     └─ (aynısı)
```

**Kilit kuralı:**

```
paylaşılan durum + ÇOK ADIMLI iş    → kilit GEREKLİ   (warm-up, cleaner)
paylaşılan durum + TEK ATOMİK komut → kilit gereksiz  (INCR sayacı)
süreç-yerel durum                    → kilit YANLIŞ olur (sweeper)
```
