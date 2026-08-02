# İstek Akışları

Bu dosya GoGinMoneyCopilot'ın çalışma zamanı akışlarını gösterir: bir istek
sisteme girdiğinde hangi katmanlardan, hangi sırayla geçtiği ve her adımda
hangi kararın verildiği.

## Notasyon

| işaret | anlamı |
|---|---|
| `╔═╗` | katman / aşama sınırı |
| `┌─┐` | tek bir adım |
| `[models.X]` | okta **akan veri** |
| `──► 4xx` | akışı kesen hata yolu |
| `╌╌►` | süreç DIŞINA çıkan çağrı — ağ turu |
| `[1] [2]` | altta numaralı açıklamaya bağlanır |

> Monospace font ve satır kaydırma kapalı olmalı.

## İçindekiler

| # | akış | kapsam |
|---|---|---|
| [0](#0--genel-topoloji) | Genel topoloji | istemci → nginx → app kopyaları → dış sistemler |
| [1](#1--kurulum-ve-kapanış) | Kurulum ve kapanış | `main()` → sinyal → graceful shutdown |
| [2](#2--post-login) | `POST /login` | kimlik doğrulama, token çifti üretimi |
| [3](#3--post-transactions) | `POST /transactions` | CRUD temsilcisi — 16 endpoint aynı kalıp |
| [4](#4--post-authrefresh) | `POST /auth/refresh` | rotation, sızıntı tespiti |
| [5](#5--post-authlogout) | `POST /auth/logout` | tek istekte üç ayrı iptal |
| [6](#6--post-chat) | `POST /chat` | AI katmanı, beyaz liste, üç risk ailesi |
| [7](#7--post-actionsconfirm) | `POST /actions/confirm` | onay token'ı, TOCTOU |
| [8](#8--arka-plan-işleri) | Arka plan işleri | periyodik görevler, lider kilitleri |

**Okuma sırası** yukarıdaki gibi: önce neyin var olduğu, sonra neyin çalıştığı.
**Anlatım sırası** farklı olabilir — canlı anlatırken 6'dan (`/chat`) başlayıp
"bunlar 1'de kuruluyor" diye geri dönmek daha etkili olur.

---

## 0 — Genel topoloji

Aşağıdaki endpoint diyagramları bu topolojiyi varsayar ve tekrar çizmez.

```
                            ┌────────────────────┐
                            │      İSTEMCİ       │
                            │ tarayıcı / Postman │
                            └─────────┬──────────┘
                                      │ :8080
            ┌─────────────────────────▼─────────────────────────┐
            │ nginx                                             │
            │ yayınlanan TEK port — app hiçbir port açmaz       │
            │ resolver 127.0.0.11 valid=10s                     │
            │ set $upstream "app:8080"   ← DNS önbelleği tuzağı │
            │ X-Forwarded-For = $remote_addr                    │
            │   ($proxy_add_x_forwarded_for DEĞİL — sahteciliği │
            │    engellemek için istemcinin değeri EZİLİR)      │
            └─────────────────────────┬─────────────────────────┘
                                      │ docker ağı
              ┌───────────────────────┼───────────────────────┐
              │                       │                       │
     ┌────────▼────────┐     ┌────────▼────────┐     ┌────────▼────────┐
     │  app kopya 1    │     │  app kopya 2    │     │  app kopya N    │
     │ TRUSTED_PROXIES │     │                 │     │                 │
     │ =172.16.0.0/12  │     │                 │     │                 │
     └────────┬────────┘     └────────┬────────┘     └────────┬────────┘
              └───────────────────────┼───────────────────────┘
                  ┌───────────────────┼───────────────────┐
                  │                   │                   │
         ┌────────▼────────┐ ┌────────▼────────┐ ┌────────▼────────┐
         │    POSTGRES     │ │      REDIS      │ │    GROQ API     │
         │                 │ │                 │ │  (dış servis)   │
         │ kalıcı kaynak   │ │ denylist kopyası│ │ LLM çağrıları   │
         │ 9 tablo         │ │ rate limit sayacı│ │ OpenAI-uyumlu  │
         │                 │ │ lider kilitleri │ │ base URL        │
         └─────────────────┘ └─────────────────┘ └─────────────────┘
```

**Neden nginx var:** app konteyneri port yayınlamıyor, bu yüzden birden fazla
kopya çalıştırılabiliyor. Tek portu nginx sahipleniyor ve gelen isteği kopyalara
dağıtıyor. Önceden `app` portu doğrudan yayınlıyordu ve ikinci kopya
"port already allocated" ile açılmıyordu.

**Neden `$remote_addr`:** `$proxy_add_x_forwarded_for` kullanılsaydı istemcinin
gönderdiği sahte değer listenin başında kalırdı ve IP bazlı hız sınırı
atlatılabilirdi. Üzerine yazmak tek güvenli seçenek.

**Neden `TRUSTED_PROXIES`:** gin varsayılan olarak tüm vekillere güvenir. Vekil
arkasındayken hangi ağın güvenilir olduğu açıkça söylenmezse ya sahte başlık
kabul edilir ya da tüm kullanıcılar tek sayaca düşer — limit ters yönde bozulur.

**Neden `resolver`:** nginx upstream adını başlangıçta bir kez çözüp önbelleğe
alır. Kopyalar yeniden başlatıldığında IP değişir ve nginx eski IP'ye gitmeye
devam eder. `resolver` + değişkenli `proxy_pass` bunu engelliyor.

---

## 1 — Kurulum ve kapanış

İstek akışı değil, **yaşam döngüsü**. Ömürde bir kez çalışır. Sıra tesadüf
değil: önce vazgeçilmez kontroller, sonra bağlantılar, sonra nesneler, en son
rotalar. Bir kontrol düşerse hiç bağlantı açılmaz.

```
╔══════════════════════════════════════════════════════════════════════════════════════╗
║ AÇILIŞ MUHAFIZLARI                       hata politikası: log.Fatal → hiç açılma     ║
║                                                                                      ║
║   ┌───────────────────────────────────────────────────────────────┐                  ║
║   │ godotenv.Load()                                           [1] │                  ║
║   │ .env dosyasını ortama basar                                   │                  ║
║   │ hata → SADECE log (Docker'da .env yok, env dışarıdan gelir)   │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
║   ┌───────────────────────────────▼───────────────────────────────┐                  ║
║   │ len(JWT_SECRET) < 32 ?                                    [2] │                  ║
║   │ "var mı" değil "yeterince güçlü mü"                           │                  ║
║   │                                                  ──► log.Fatal│                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
║   ┌───────────────────────────────▼───────────────────────────────┐                  ║
║   │ auth.ValidateCookieConfig()                               [3] │                  ║
║   │ SameSite=None + Secure=false → tarayıcı cookie'yi REDDEDER    │                  ║
║   │                                                  ──► log.Fatal│                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
║   ┌───────────────────────────────▼───────────────────────────────┐                  ║
║   │ validators.RegisterCustomValidators()                         │                  ║
║   │ gin'in doğrulayıcısına "accountname" kuralını ekler           │                  ║
║   │ regex ^[\p{L}0-9 ]+$   ← \p{L} sayesinde Türkçe harf geçer    │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
╚═══════════════════════════════════╪══════════════════════════════════════════════════╝
                                    │
╔═══════════════════════════════════▼══════════════════════════════════════════════════╗
║ BAĞLANTILAR                                                                          ║
║                                                                                      ║
║   ┌───────────────────────────────────────────────────────────────┐   ┌───────────┐  ║
║   │ database.InitDB()                                         [4] │╌╌►│ POSTGRES  │  ║
║   │ DSN'i env'den kur                                             │   │ bağlan    │  ║
║   │ 10 deneme × 2sn  ← Docker'da Postgres app'ten sonra hazır olur│   │ +migrate  │  ║
║   │ AutoMigrate(9 model) → database.DB   ──► log.Fatal            │   └───────────┘  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
║   ┌───────────────────────────────▼───────────────────────────────┐   ┌───────────┐  ║
║   │ database.InitRedis()                                      [5] │╌╌►│   REDIS   │  ║
║   │ REDIS_ADDR boş  → nil döner, RDB nil kalır (özellik kapalı)   │   │  PING     │  ║
║   │ REDIS_ADDR dolu → NewClient + PING (5sn)                      │   └───────────┘  ║
║   │ ulaşılamıyorsa                                   ──► log.Fatal│                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
╚═══════════════════════════════════╪══════════════════════════════════════════════════╝
                                    │
╔═══════════════════════════════════▼══════════════════════════════════════════════════╗
║ NESNELERİN KURULMASI                                    composition root             ║
║                                                                                      ║
║   ┌───────────────────────────────────────────────────────────────┐                  ║
║   │ repositories × 8                                              │                  ║
║   │ New*Repository(database.DB) → hepsi ARAYÜZ döndürür           │                  ║
║   │ bu satırdan sonra kimse gorm'un varlığını bilmez              │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
║   ┌───────────────────────────────▼───────────────────────────────┐                  ║
║   │ sweeperStop := make(chan struct{})                            │                  ║
║   │ üç arka plan işinin ORTAK kapatma sinyali                     │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
║   ┌───────────────────────────────▼───────────────────────────────┐   ┌───────────┐  ║
║   │ DENYLIST ZİNCİRİ   (yalnızca RDB != nil)                  [6] │╌╌►│   REDIS   │  ║
║   │  ① WarmUpDenylist(...)  SENKRON, 10sn timeout                 │   │ N × SET   │  ║
║   │     hata → ölümcül DEĞİL, okumalar Postgres'e düşer           │   │ + nöbetçi │  ║
║   │  ② go StartWarmUpLoop(...)          ───► arka plan            │   └───────────┘  ║
║   │  ③ tokenRepo = NewRedisTokenRepository(RDB, tokenRepo)        │                  ║
║   │     ↑ decorator: TokenRepository alır, TokenRepository döner  │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
║   ┌───────────────────────────────▼───────────────────────────────┐                  ║
║   │ AI ZİNCİRİ                                                [7] │                  ║
║   │  ai.NewGroqParser()   GROQ_API_KEY yoksa hata                 │                  ║
║   │    hata → chatService nil KALIR, uygulama normal açılır       │                  ║
║   │  chat.NewActionService(parser, 5 repo) → *ActionService       │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
║   ┌───────────────────────────────▼───────────────────────────────┐                  ║
║   │ handlers × 6                                                  │                  ║
║   │ NewChatHandler(chatService)  ← nil olabilir, handler 503 döner│                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
║   ┌───────────────────────────────▼───────────────────────────────┐                  ║
║   │ RATE LIMITER'LAR                                          [8] │                  ║
║   │  authMem = NewRateLimiter(authPerMin, burst=5)                │                  ║
║   │  chatMem = NewRateLimiter(chatDefaultLimit, burst=5)          │                  ║
║   │  go authMem.StartSweeper(sweeperStop)   ───► arka plan        │                  ║
║   │  go chatMem.StartSweeper(sweeperStop)   ───► arka plan        │                  ║
║   │  RDB varsa → NewRedisLimiter(..., fallback=mem) ile sarmala   │                  ║
║   │  var authLimiter Limiter      ← DAR, AllowWithLimit gizlenir  │                  ║
║   │  var chatLimiter FullLimiter  ← GENİŞ                         │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
║   ┌───────────────────────────────▼───────────────────────────────┐                  ║
║   │ chatPlanFn closure                                        [9] │                  ║
║   │ userRepo'yu YAKALAR — middleware repositories'i import etmez  │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
╚═══════════════════════════════════╪══════════════════════════════════════════════════╝
                                    │
╔═══════════════════════════════════▼══════════════════════════════════════════════════╗
║ MOTOR VE ROTALAR                                                                     ║
║                                                                                      ║
║   ┌───────────────────────────────────────────────────────────────┐                  ║
║   │ gin.New()      ← Default() DEĞİL, kendi logger'ımız var       │                  ║
║   │ SetupTrustedProxies(r)                          ──► log.Fatal │                  ║
║   │   TRUSTED_PROXIES boş → SetTrustedProxies(nil)                │                  ║
║   │   = hiçbir vekile güvenme, gerçek TCP adresini kullan    [10] │                  ║
║   │ r.Use(RequestLogger) · r.Use(gin.Recovery())                  │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
║   ┌───────────────────────────────▼───────────────────────────────┐                  ║
║   │ public rotalar × 3     /register · /login · /auth/refresh     │                  ║
║   │   hepsi Limit(authLimiter, KeyByIP) ile                       │                  ║
║   ├───────────────────────────────────────────────────────────────┤                  ║
║   │ authorized := r.Group("/")                                    │                  ║
║   │ authorized.Use(AuthMiddleware(tokenRepo))   ← GRUBUN TAMAMINA │                  ║
║   │   /auth/logout · /chat · /actions/confirm                     │                  ║
║   │   /accounts/* · /categories/* · /transactions/* · /budgets/*  │                  ║
║   │   yalnızca /chat ayrıca LimitByPlan alır                 [11] │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
║   ┌───────────────────────────────▼───────────────────────────────┐                  ║
║   │ BAKIM İŞÇİSİ                                                  │                  ║
║   │ cleanupCtx, stopCleanup := context.WithCancel(...)            │                  ║
║   │ NewCleaner(...).UseRedisLock(database.RDB)                    │                  ║
║   │ go cleaner.Start(cleanupCtx)            ───► arka plan        │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
║   ┌───────────────────────────────▼───────────────────────────────┐                  ║
║   │ go srv.ListenAndServe()   :8080         ───► arka plan        │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
╚═══════════════════════════════════╪══════════════════════════════════════════════════╝
                                    │
╔═══════════════════════════════════▼══════════════════════════════════════════════════╗
║ BEKLEME                                                                              ║
║   quit := make(chan os.Signal, 1)                                                    ║
║   signal.Notify(quit, os.Interrupt, syscall.SIGTERM)                                 ║
║   <-quit                    ← program BURADA durur, 5 goroutine arka planda çalışır  ║
╚═══════════════════════════════════╤══════════════════════════════════════════════════╝
                                    │ SIGINT / SIGTERM
╔═══════════════════════════════════▼══════════════════════════════════════════════════╗
║ KAPANIŞ                          sıra: önce üreticiler, sonra sunucu, sonra bağlantı ║
║                                                                                      ║
║   ┌───────────────────────────────────────────────────────────────┐                  ║
║   │ stopCleanup()          → cleaner ctx.Done() görür, çıkar      │                  ║
║   │ close(sweeperStop)     → 3 goroutine BİRDEN çıkar        [12] │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
║   ┌───────────────────────────────▼───────────────────────────────┐                  ║
║   │ srv.Shutdown(ctx 5sn)                                    [13] │                  ║
║   │ yeni bağlantı alma, AÇIK istekleri bitir                      │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
║   ┌───────────────────────────────▼───────────────────────────────┐                  ║
║   │ sqlDB.Close()          → Postgres havuzu                      │                  ║
║   │ database.CloseRedis()  → Redis istemcisi                      │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
║                                   ▼                                                  ║
║                        "Server exited gracefully"                                    ║
╚══════════════════════════════════════════════════════════════════════════════════════╝
```

**[1]** `.env` yokluğu hata değil — Docker Compose'da değişkenler dışarıdan gelir.

**[2]** HMAC-SHA256 için 32 karakter ~256 bit. Bu kontrol olmasaydı `JWT_SECRET=test`
ile üretime çıkılabilir, saldırgan anahtarı bulup **istediği kullanıcı için token
imzalayabilirdi** — sistemdeki her kimlik kontrolü çökerdi.

**[3]** Tarayıcılar `SameSite=None` olan cookie'yi `Secure` değilse sessizce
reddeder. Kullanıcı "giriş yapamıyorum" der, sebebi hiçbir logda görünmez. Açılışta
yakalanınca 3 saniyede anlaşılır. Sessiz başarısızlığı gürültülü hâle getirme
refleksi.

**[4]** Retry döngüsü olmasaydı `docker compose up` yarı yarıya çökerdi — Postgres
uygulamadan sonra hazır olabiliyor.

**[5]** İki farklı davranış: **adres yoksa** özellik bilinçli kapalı, hata yok.
**Adres varsa ama ulaşılamıyorsa** ölümcül — bu yanlış yapılandırma ve sessiz
geçilirse "Redis var sanıyordum" durumu doğar. `NewClient` hiçbir şey bağlamaz,
`PING` olmasaydı hata ilk `IsRevoked`'da, yani trafik altında patlardı.

**[6]** Sıra önemli: warm-up **sarmalamadan önce** çalışıyor. Önce kopyayı doldur,
sonra ona güvenmeye başla. Ve hata ölümcül değil — nöbetçi yazılmazsa okumalar
Postgres'e düşer, yavaş ama doğru.

**[7]** `GROQ_API_KEY` yoksa `chatService` nil kalır ve uygulama normal açılır.
`/chat` 503 döner, geri kalan her şey çalışır. `JWT_SECRET` ile karşılaştır: biri
ölümcül, diğeri değil. **Ayıran kural: eksik olan şey doğruluğu bozuyorsa açılma;
yalnızca bir özelliği kaybettiriyorsa aç ve logla.**

**[8]** `var authLimiter middleware.Limiter = authMem` — `authMem` fiilen
`AllowWithLimit`'e sahip ama dar arayüzle ilan edilerek o yetenek **bilerek atıldı**.
Sonuç: `LimitByPlan(authLimiter, ...)` derlenmez. `/login`'in plana göre limit
uygulaması imkânsız — unutulduğu için değil, derleyici izin vermediği için.

**[9]** Closure `userRepo`'yu yakalıyor, böylece `middleware` paketi
`repositories`'i import etmek zorunda kalmıyor. Bağımlılık yönü korunuyor.

**[10]** Gin varsayılan olarak tüm vekillere güvenir ve `X-Forwarded-For`'u okur.
Önde vekil yokken o başlığı istemci yazar — her isteğe farklı IP koyup IP bazlı
limiti tamamen atlatır. Ölçüldü: 8 istek, hiç 429 yok.

**[11]** `AuthMiddleware` gruba (kimlik her yerde gerekli), `LimitByPlan` tek
rotaya (para sadece `/chat`'te harcanıyor).

**[12]** İki ayrı durdurma mekanizması, çünkü iki farklı ihtiyaç var. `Cleaner`
bir `context` alıyor ve onu repository çağrılarına da geçiriyor — iptal aşağı
yayılıyor. Sweeper'lar ve warm-up döngüsü sadece "dur" sinyali istiyor;
`close(chan)` üç goroutine'i aynı anda uyandırıyor.

**[13]** `srv.Close()` deseydin işlenen istekler yarıda kesilirdi. `Shutdown`
yeni bağlantı kabul etmeyi bırakıp açık olanları 5 saniyeye kadar bekliyor.

---

## 2 — POST /login

```
╔══════════════════════════════════════════════════════════════════════════════════════╗
║ İSTEK                                                                                ║
║   POST /login          { "username": "...", "password": "..." }                      ║
║   istemci → nginx (tek yayın portu :8080) → app:8080                                 ║
║   nginx: X-Forwarded-For = $remote_addr   ← istemcinin yazdığı değeri EZER           ║
╚══════════════════════════════════════╤═══════════════════════════════════════════════╝
                                       │
╔══════════════════════════════════════▼═══════════════════════════════════════════════╗
║ MIDDLEWARE ZİNCİRİ                                        public rota — kimlik yok   ║
║                                                                                      ║
║   ┌───────────────────────────────────────────────────────────────┐                  ║
║   │ RequestLogger                                                 │                  ║
║   │ süre sayacını başlatır · c.Next() sonrası yapılandırılmış log │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
║                                   │                                                  ║
║   ┌───────────────────────────────▼───────────────────────────────┐                  ║
║   │ gin.Recovery                                                  │                  ║
║   │ panik olursa yakalar → 500 · sunucu ayakta kalır              │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
║                                   │                                                  ║
║   ┌───────────────────────────────▼───────────────────────────────┐   ┌───────────┐  ║
║   │ Limit(authLimiter, KeyByIP)                               [1] │╌╌►│   REDIS   │  ║
║   │ INCR  rl:auth:ip:<ip>:<dakika>   +   EXPIRE                   │   │ INCR      │  ║
║   │ IP başına SABİT eşik · AUTH_RATE_PER_MIN=10                   │   │ EXPIRE    │  ║
║   │ Redis yoksa bellekteki jeton kovasına düşer                   │   │ (yedek:   │  ║
║   │                                              aşıldı ──► 429   │   │  bellek)  │  ║
║   └───────────────────────────────┬───────────────────────────────┘   └───────────┘  ║
╚═══════════════════════════════════╪══════════════════════════════════════════════════╝
                                    │
╔═══════════════════════════════════▼══════════════════════════════════════════════════╗
║ HANDLER — authHandler.Login                             HTTP ↔ domain çevirisi       ║
║                                                                                      ║
║   ┌───────────────────────────────────────────────────────────────┐                  ║
║   │ ShouldBindJSON(&input)                                    [2] │                  ║
║   │ binding: username required · password required                │                  ║
║   │ ShouldBind DEĞİL — form-encoded kabul edilmesin diye          │                  ║
║   │                                            geçersiz ──► 400   │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
║                                   │ [models.LoginInput]                              ║
║   ┌───────────────────────────────▼───────────────────────────────┐   ┌───────────┐  ║
║   │ users.GetByUsername(ctx, input.Username)                  [3] │╌╌►│ POSTGRES  │  ║
║   │ SELECT * FROM users WHERE username = ? LIMIT 1                │   │ SELECT    │  ║
║   │ ErrRecordNotFound → ErrUserNotFound                           │   └───────────┘  ║
║   │                                       altyapı hatası ──► 500  │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
║                                   │                                                  ║
║                ┌──────────────────┴───────────────────┐                              ║
║           kullanıcı YOK                        kullanıcı VAR                         ║
║                │                                      │ [*models.User]               ║
║   ┌────────────▼──────────────────┐   ┌───────────────▼───────────────┐              ║
║   │ CheckPassword(pw, dummyHash)  │   │ CheckPassword(                │              ║
║   │                           [4] │   │   pw, user.PasswordHash)      │              ║
║   │ sonuç KULLANILMAZ, atılır     │   │ gerçek karşılaştırma          │              ║
║   │ bcrypt — kasten yavaş         │   │ bcrypt — kasten yavaş         │              ║
║   │ AĞ YOK · saf CPU              │   │ AĞ YOK · saf CPU              │              ║
║   │ amaç: iki yolun SÜRESİ eşit   │   └────────┬─────────────────┬────┘              ║
║   └────────────┬──────────────────┘        eşleşmedi         eşleşti                 ║
║                └──────────────┬─────────────────┘                │                   ║
║   ┌───────────────────────────▼───────────────────────────┐      │                   ║
║   │ 401  "Username or Password is wrong!"             [5] │      │                   ║
║   │ iki başarısızlık yolu → AYNI mesaj, AYNI süre         │      │                   ║
║   └───────────────────────────────────────────────────────┘      │                   ║
╚══════════════════════════════════════════════════════════════════╪═══════════════════╝
                                                                   │ [*models.User]
╔══════════════════════════════════════════════════════════════════▼═══════════════════╗
║ TOKEN ÜRETİMİ — issueTokenPair                       /auth/refresh ile ORTAK KOD     ║
║                                                                                      ║
║   ┌───────────────────────────────────────────────────────────────┐                  ║
║   │ auth.GenerateToken(user.ID, user.Role)                    [6] │                  ║
║   │ generateJTI: 16 byte crypto/rand → hex                        │                  ║
║   │ claims{ user_id, role, jti, exp=now+AccessTokenTTL() }        │                  ║
║   │ HS256 imza · anahtar = jwtSecret()          hata ──► 500      │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
║                                   │ [access token]                                   ║
║   ┌───────────────────────────────▼───────────────────────────────┐                  ║
║   │ auth.NewRefreshToken()                                    [7] │                  ║
║   │ 32 byte crypto/rand → base64        = raw   (cookie'ye)       │                  ║
║   │ SHA-256(raw) → hex                  = hash  (veritabanına)    │                  ║
║   │                                             hata ──► 500      │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
║                                   │ [raw, hash]                                      ║
║   ┌───────────────────────────────▼───────────────────────────────┐   ┌───────────┐  ║
║   │ refresh.Create({UserID, TokenHash: hash, ExpiresAt})      [8] │╌╌►│ POSTGRES  │  ║
║   │ ham değer ASLA yazılmaz — yalnızca hash saklanır              │   │ INSERT    │  ║
║   │                                             hata ──► 500      │   └───────────┘  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
║                                   │                                                  ║
║   ┌───────────────────────────────▼───────────────────────────────┐                  ║
║   │ auth.SetRefreshCookie(c, raw)                             [9] │                  ║
║   │ c.SetSameSite(...)  ← SetCookie'den ÖNCE çağrılmalı           │                  ║
║   │ name=refresh_token · maxAge=RefreshTokenTTL()                 │                  ║
║   │ Path=/auth · HttpOnly · Secure                                │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
╚═══════════════════════════════════╪══════════════════════════════════════════════════╝
                                    │
╔═══════════════════════════════════▼══════════════════════════════════════════════════╗
║ CEVAP                                                                                ║
║   200   { "token": "<access token>" }                                                ║
║         Set-Cookie: refresh_token=<raw>; HttpOnly; Secure; Path=/auth                ║
╚══════════════════════════════════════════════════════════════════════════════════════╝
```

**[1]** Kimlik henüz yok, elimizdeki tek ayırt edici şey IP. Limitin koruduğu şey
aşağıdaki bcrypt'in CPU'su. `/transactions`'ta limiter olmamasının sebebi de bu —
orada sıradan bir `INSERT` var, pahalı bir hesap yok.

**[2]** `ShouldBind` form-encoded veriyi de kabul ederdi. Başka bir sitedeki gizli
HTML formu `/login`'e POST atabilir (tarayıcı form gönderimini engellemez), ama
`Content-Type: application/json` gönderemez. Sadece JSON kabul etmek bedava bir
yapısal duvar.

**[3]** Altyapı hatası ile "kullanıcı yok" ayrılıyor. Postgres düştüğünde 401
dönseydi herkes şifresini yanlış hatırladığını sanır, arıza sinyali kaybolurdu.

**[4]** Bu diyagramın yıldızı. Kullanıcı bulunamasa bile bcrypt çalışıyor, sonucu
atılıyor. Olmasaydı: olmayan kullanıcı hemen 401 alır, var olan kullanıcı bcrypt
kadar bekler. Saldırgan cevap sürelerini karşılaştırıp **hangi kullanıcı adlarının
var olduğunu** çıkarırdı. `dummyHash` paket seviyesinde bir kez hesaplanıyor, her
istekte değil.

**[5]** Projedeki üçüncü tekrarı: `/actions/confirm` dört başarısızlığa tek cevap
veriyor, `/auth/refresh` sızıntı ile geçersizliği ayırmıyor. Burada mesajı
eşitlemek yetmiyor — ölçülebilir ikinci bir kanal olduğu için **süreyi de**
eşitlemek gerekiyor.

**[6]** `jti` her token'da yeni. Logout'ta denylist'e yazılacak değer bu —
token'ın kimliği.

**[7]** İki farklı hash, iki gerekçe. Parola bcrypt ile: düşük entropili, tahmin
edilebilir, yavaşlık koruma. Refresh token SHA-256 ile: 32 byte tam entropi,
tahmin edilecek bir şey yok, ve her yenilemede aranacağı için hızlı olmalı.

**[8]** Ham değer cookie'ye gidiyor, hash veritabanına. DB sızsa saldırganın
elinde geri çevrilemez hash'ler olur.

**[9]** Gin `SetSameSite`'ı **bir sonraki** `SetCookie` çağrısında uyguluyor.
Sırayı ters yazsaydın ayar sessizce düşerdi — hata da vermezdi.

**Ortak çıkış.** `issueTokenPair` hem `/login`'in hem `/auth/refresh`'in son
bloğu. Token çifti üretme kuralı tek yerde yaşıyor; TTL değişse ya da rotation'a
bir adım eklense iki yeri güncellemek gerekmiyor.

---

## 3 — POST /transactions

Sıradan CRUD'un temsilcisi. `/accounts`, `/categories`, `/budgets` altındaki
16 endpoint de aynı kalıbı izler: bağla → sahipliği doğrula → repository'ye
devret → durum kodu seç.

Bu diyagramın değeri **çıplaklığında**: AI yok, rate limiter yok, token yok.
Diğer akışlardaki fazladan makinenin ne kadar fazladan olduğu ancak buna
bakınca görünür.

```
╔══════════════════════════════════════════════════════════════════════════════════════╗
║ İSTEK                                                                                ║
║   POST /transactions                                                                 ║
║   { "account_id": 3, "category_id": 1, "amount": 149.9, "type": "expense",           ║
║     "description": "kahve", "transaction_date": "2026-07-30T12:00:00Z" }             ║
╚══════════════════════════════════════╤═══════════════════════════════════════════════╝
                                       │
╔══════════════════════════════════════▼═══════════════════════════════════════════════╗
║ MIDDLEWARE ZİNCİRİ                              korumalı rota — rate limiter YOK     ║
║                                                                                      ║
║   ┌───────────────────────────────────────────────────────────────┐                  ║
║   │ RequestLogger  ·  gin.Recovery                                │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
║   ┌───────────────────────────────▼───────────────────────────────┐   ┌───────────┐  ║
║   │ AuthMiddleware(tokenRepo)                                 [1] │╌╌►│REDIS/PG   │  ║
║   │  ① Authorization header var mı?              yoksa ──► 401    │   │ IsRevoked │  ║
║   │  ② "Bearer <x>" formatı?                     değilse ──► 401  │   └───────────┘  ║
║   │  ③ auth.ValidateToken → imza + süre          hata ──► 401     │                  ║
║   │  ④ tokens.IsRevoked(jti)   ← tek I/O yapan adım               │                  ║
║   │       iptal ──► 401     ·     sorgu hatası ──► 500 (fail-closed)                 ║
║   │  ⑤ c.Set: user_id · role · jti · token_exp                    │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
╚═══════════════════════════════════╪══════════════════════════════════════════════════╝
                                    │
╔═══════════════════════════════════▼══════════════════════════════════════════════════╗
║ HANDLER — transactionHandler.CreateTransaction                                       ║
║                                                                                      ║
║   ┌───────────────────────────────────────────────────────────────┐                  ║
║   │ ShouldBindJSON(&input)                                    [2] │                  ║
║   │ account_id required · category_id required                    │                  ║
║   │ amount required,gt=0 · type oneof(income expense)             │                  ║
║   │ description max=100 · transaction_date required (RFC3339)     │                  ║
║   │                                            geçersiz ──► 400   │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
║                                   │ [models.CreateTransactionInput]                  ║
║   ┌───────────────────────────────▼───────────────────────────────┐   ┌───────────┐  ║
║   │ getAccountForRequest(c, h.accounts, input.AccountID)      [3] │╌╌►│ POSTGRES  │  ║
║   │   role := c.MustGet("role")                                   │   │ SELECT    │  ║
║   │   admin  → accounts.GetByID(accountID)                        │   └───────────┘  ║
║   │   değil  → accounts.GetByIDForUser(accountID, userID)         │                  ║
║   │            ↑ SAHİPLİK sorgunun İÇİNDE                         │                  ║
║   │   bulunamadı ──► 404   (403 değil — varlık sızdırmamak için)  │                  ║
║   │   diğer hata ──► 500                                          │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
║                                   │                                                  ║
║   ┌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌▼╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌┐                  ║
║   ╎ category_id için SAHİPLİK KONTROLÜ YOK                   [4] ╎  ← BULGU          ║
║   ╎ handlers/transaction_handlers.go içinde "categor" geçmiyor    ╎                  ║
║   └╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌┬╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌┘                  ║
║                                   │                                                  ║
║   ┌───────────────────────────────▼───────────────────────────────┐   ┌───────────┐  ║
║   │ transactions.Create(ctx, input)                           [5] │╌╌►│ POSTGRES  │  ║
║   │ input → models.Transaction · db.Create(&tx)                   │   │ INSERT    │  ║
║   │ FK    : account_id → accounts · category_id → categories      │   └───────────┘  ║
║   │ CHECK : type IN ('income','expense')                          │                  ║
║   │ numeric(12,2) : amount                       hata ──► 500     │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
╚═══════════════════════════════════╪══════════════════════════════════════════════════╝
                                    │
╔═══════════════════════════════════▼══════════════════════════════════════════════════╗
║ CEVAP                                                                                ║
║   201   { "message": "Transaction created!" }                                        ║
║   NOT: oluşan kaydın id'si DÖNMÜYOR — client referans vermek isterse                 ║
║        fazladan bir GET atmak zorunda                                           [6]  ║
╚══════════════════════════════════════════════════════════════════════════════════════╝
```

**[1]** Adımlar **ucuzdan pahalıya** sıralı. Uydurma bir token 3. adımda düşer ve
sunucuya yalnızca bir HMAC hesabı yaptırır. Sıra ters olsaydı aynı saldırı her
istekte bir veritabanı sorgusu üretirdi. **Reddedilmek bedava değildir; hangi
kaynağı harcadığını sıralama belirler.**

`IsRevoked` hata verdiğinde 401 değil **500** dönüyor — "iptal edilmemiş
varsayalım" demiyoruz. Doğrulayamıyorsak reddediyoruz (fail-closed).

**[2]** Doğrulamanın tamamı struct etiketlerinde. `binding` yalnızca HTTP
gövdesi JSON'dan çözülürken çalışır — servisi Go içinden çağıran biri bu
kontrolden geçmez.

**[3]** `userID` istek **gövdesinden gelmiyor**, context'ten geliyor — oraya
`AuthMiddleware` koydu, o da imzası doğrulanmış JWT'den okudu. Gövdeden gelseydi
herkes `{"user_id": 7}` yazıp başkası adına kayıt oluştururdu.

Rolüne göre **farklı repository metodu** çağrılıyor ve sahiplik SQL'in `WHERE`'ünde.
Başkasının hesabı istendiğinde 404 dönüyor, 403 değil — 403 "bu hesap var ama
senin değil" bilgisini sızdırırdı.

**[4] BULGU.** `account_id` için sahiplik doğrulanıyor, `category_id` için
doğrulanmıyor. FK sayesinde kategori **var olmak** zorunda ama **kimin olduğu**
kontrol edilmiyor.

Karşılaştır: `chat` tarafındaki `resolveCategory` bu kontrolü titizce yapıyor
(`cat.UserID == nil → ErrGlobalCategory`, `*cat.UserID != userID →
ErrCategoryNotFound`). Yani **AI yolu, REST yolundan daha sıkı.**

Etkisi ölçülü: veri ihlali değil (B, A'nın işlemini göremez — listeleme hesap
üzerinden, hesaplar kullanıcı üzerinden kapsanıyor). Ama iki gerçek sonuç var:
bütünlük açığı (A'nın işlemi A'nın olmayan bir kategoriye etiketlenir ve bütçe
hesabında sessizce sayılmaz) ve zayıf bir varlık kehaneti (FK ihlali 500, başarı
201 → hangi id'lerin var olduğu öğrenilebilir).

Doğru çözüm: `chat`'teki mantığı ortak bir yere alıp iki taraftan da çağırmak.
Handler'a ayrı bir kopya yazmak üçüncü bir ayrışma noktası yaratır.

**[5]** Bu, projedeki **tek yazma kapısı**. `/chat` işlem yazmıyor, taslak
üretiyor ve frontend bu rotaya gönderiyor. Doğrulama, sahiplik ve iş kuralları
bu yüzden tek yerde yaşayabiliyor.

**[6]** `201` yalnızca mesaj döndürüyor, `id` döndürmüyor. REST alışkanlığı
oluşan kaynağı (ya da en az `id`'yi) ve bir `Location` başlığı döndürmektir.
Aynısı categories, accounts ve budgets için de geçerli.

---

## 4 — POST /auth/refresh

Şekli diğerlerine benzemiyor: burada kimliğin kendisi yenileniyor. İçinde
**rotation** ve **sızıntı tespiti** var.

```
╔══════════════════════════════════════════════════════════════════════════════════════╗
║ İSTEK                                                                                ║
║   POST /auth/refresh          gövde YOK                                              ║
║   Cookie: refresh_token=<raw>                                                        ║
╚══════════════════════════════════════╤═══════════════════════════════════════════════╝
                                       │
╔══════════════════════════════════════▼═══════════════════════════════════════════════╗
║ MIDDLEWARE ZİNCİRİ                                                                   ║
║                                                                                      ║
║   ┌───────────────────────────────────────────────────────────────┐                  ║
║   │ RequestLogger  ·  gin.Recovery                                │                  ║
║   │ Limit(authLimiter, KeyByIP)                  aşıldı ──► 429   │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
║                                   │                                                  ║
║   ⚠ AuthMiddleware YOK  — buraya access token'ın süresi dolduğu için gelindi.   [1]  ║
║     Geçerli access token istemek döngüsel olurdu. Kimlik cookie'den gelir.           ║
╚═══════════════════════════════════╪══════════════════════════════════════════════════╝
                                    │
╔═══════════════════════════════════▼══════════════════════════════════════════════════╗
║ HANDLER — authHandler.Refresh                                                        ║
║                                                                                      ║
║   ┌───────────────────────────────────────────────────────────────┐                  ║
║   │ auth.RefreshTokenFromRequest(c)                               │                  ║
║   │ c.Cookie("refresh_token") · hata → "" döner                   │                  ║
║   │                                              boşsa ──► 401    │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
║                                   │ [raw]                                            ║
║   ┌───────────────────────────────▼───────────────────────────────┐                  ║
║   │ auth.HashRefreshToken(raw)                                [2] │                  ║
║   │ SHA-256 → hex · DB'de ham değer değil HASH aranır             │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
║                                   │ [hash]                                           ║
║   ┌───────────────────────────────▼───────────────────────────────┐   ┌───────────┐  ║
║   │ refresh.Consume(ctx, hash, now)      TEK ATOMİK UPDATE    [3] │╌╌►│ POSTGRES  │  ║
║   │ UPDATE refresh_tokens SET used_at = now                       │   │ UPDATE    │  ║
║   │ WHERE token_hash=? AND used_at IS NULL                        │   │ (+SELECT  │  ║
║   │       AND revoked_at IS NULL AND expires_at > ?               │   │  teşhis)  │  ║
║   │                                                               │   └───────────┘  ║
║   │ RowsAffected == 1 → kaydı getir             ✓ [*RefreshToken] │                  ║
║   │ RowsAffected == 0 → NEDEN? ikinci sorgu:                  [4] │                  ║
║   │    kayıt hiç yok      → ErrRefreshTokenInvalid   record=nil   │                  ║
║   │    UsedAt != nil      → ErrRefreshTokenReused    record=DOLU  │                  ║
║   │    iptal / süresi doldu → ErrRefreshTokenInvalid record=nil   │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
║                       ┌───────────┴────────────┐                                     ║
║                    hata                     başarı                                   ║
║                       │                        │ [*models.RefreshToken]              ║
║   ┌───────────────────▼───────────────────┐    │              ┌───────────┐          ║
║   │ handleRefreshFailure              [5] │    │         ╌╌╌╌►│ POSTGRES  │          ║
║   │ Reused && record != nil ?             │    │              │ UPDATE    │          ║
║   │   → refresh.RevokeAllForUser(userID)  │    │              │ (toplu)   │          ║
║   │     TÜM oturumlar kapanır             │    │              └───────────┘          ║
║   │     log "SECURITY: refresh reused"    │    │                                     ║
║   │ ClearRefreshCookie(c)                 │    │                                     ║
║   │ 401 "Session expired…"  ← HER DURUMDA │    │                                     ║
║   │    AYNI cevap                         │    │                                     ║
║   └───────────────────────────────────────┘    │                                     ║
║                                                │                                     ║
║   ┌────────────────────────────────────────────▼──────────────────┐   ┌───────────┐  ║
║   │ users.GetByID(ctx, record.UserID)      ROLÜ TAZE OKU      [6] │╌╌►│ POSTGRES  │  ║
║   │ kullanıcı silinmiş → ClearRefreshCookie + 401                 │   │ SELECT    │  ║
║   └───────────────────────────────┬───────────────────────────────┘   └───────────┘  ║
╚═══════════════════════════════════╪══════════════════════════════════════════════════╝
                                    │ [*models.User]
╔═══════════════════════════════════▼══════════════════════════════════════════════════╗
║ TOKEN ÜRETİMİ — issueTokenPair                            /login ile ORTAK KOD  [7]  ║
║   auth.GenerateToken(...)      → yeni access token, YENİ jti                         ║
║   auth.NewRefreshToken()       → yeni raw + hash                                     ║
║   refresh.Create(hash)     ╌╌► POSTGRES INSERT                                       ║
║   auth.SetRefreshCookie(raw)   → yeni cookie                        hata ──► 500     ║
╚═══════════════════════════════════╤══════════════════════════════════════════════════╝
                                    │
╔═══════════════════════════════════▼══════════════════════════════════════════════════╗
║ CEVAP                                                                                ║
║   200   { "token": "<yeni access token>" }                                           ║
║         Set-Cookie: refresh_token=<yeni raw>; HttpOnly; Path=/auth                   ║
╚══════════════════════════════════════════════════════════════════════════════════════╝
```

**[1]** Rota `authorized` grubunda değil. Buraya access token'ın süresi dolduğu
için gelinir; geçerli bir access token şart koşmak döngüsel olurdu. Rota public
ama korumasız değil — cookie'nin kendisi kanıt.

**[2]** DB'de ham değer yok, `token_hash` var. Veritabanı sızsa saldırganın
elinde hash'ler olur ve SHA-256 tek yönlü.

**Neden SHA-256, parolalardaki gibi bcrypt değil?** Refresh token 32 byte
`crypto/rand` — tam entropi, tahmin edilecek bir şey yok. Bcrypt'in yavaşlığı
düşük entropili insan parolalarını sözlük saldırısından korumak için var.
Üstelik bu arama her yenilemede çalışıyor, hızlı olmalı.

**[3]** `Claim` ile aynı desen: tek atomik `UPDATE`. `SELECT`-sonra-`UPDATE`
olsaydı iki eşzamanlı istek de geçer, token iki kez tüketilirdi. `used_at IS NULL`
koşulunu yalnızca bir istek sağlayabilir.

**[4]** Ama `Claim`'den bir farkla: başarısız olunca **ikinci bir sorgu** atıp
nedeni araştırıyor. Çünkü "tekrar kullanım" ile "geçersiz" **farklı güvenlik
tepkisi** gerektiriyor — biri tüm oturumları kapatıyor, diğeri kapatmıyor.

Dikkat: teşhis yapılıyor ama **client'a giden cevap yine aynı**.
*İçeride teşhis etmek ile dışarıya açıklamak farklı şeylerdir.*

Ve hatayla birlikte **kayıt da** dönüyor (`return &existing, ErrRefreshTokenReused`)
— Go'da alışılmadık, ama çağıranın `UserID`'ye ihtiyacı var.

**[5]** Tüketilmiş bir token tekrar geldi: ya saldırgan çaldı ya meşru kullanıcı
eskisini oynatıyor. **Ayırt edilemez.** Sadece o token'ı iptal etmek yetmez —
saldırgan zaten en yeni token'ı almış olabilir. Tek etkili tepki hepsini kapatmak.

**[6]** Rol refresh token'ın içinde değil. Gömseydik, yetkisi alınmış bir kullanıcı
token geçerli olduğu sürece eski yetkisini korurdu. `/chat`'teki plan okumasıyla
aynı gerekçe.

**[7]** Rotation: her yenileme yeni bir çift üretiyor. Bu olmasaydı tekrar kullanım
tespit **edilemezdi** — aynı token'ın iki kez gelmesi normal olurdu.

---

## 5 — POST /auth/logout

Tek istekte **üç ayrı iptal**, üçü de farklı yerde yaşıyor.

```
╔══════════════════════════════════════════════════════════════════════════════════════╗
║ İSTEK                                                                                ║
║   POST /auth/logout           gövde YOK                                              ║
║   Authorization: Bearer <access>     ·     Cookie: refresh_token=<raw>               ║
╚══════════════════════════════════════╤═══════════════════════════════════════════════╝
                                       │
╔══════════════════════════════════════▼═══════════════════════════════════════════════╗
║ MIDDLEWARE ZİNCİRİ                                                                   ║
║   RequestLogger · gin.Recovery                                                       ║
║   AuthMiddleware(tokenRepo)   ← access token ZORUNLU                            [1]  ║
║     header / format / ValidateToken / IsRevoked        ──► 401  ·  500               ║
║     c.Set: user_id · role · jti · token_exp                                          ║
╚══════════════════════════════════════╤═══════════════════════════════════════════════╝
                                       │
╔══════════════════════════════════════▼═══════════════════════════════════════════════╗
║ HANDLER — authHandler.Logout                                                         ║
║                                                                                      ║
║   jti := c.MustGet("jti").(string)          ← AuthMiddleware koydu                   ║
║   exp := c.MustGet("token_exp").(time.Time) ← denylist TTL'i buradan            [2]  ║
║                                                                                      ║
║   ┌───────────────────────────────────────────────────────────────┐   ┌───────────┐  ║
║   │ [A] tokens.Revoke(ctx, jti, exp)          ACCESS TOKEN    [3] │╌╌►│ POSTGRES  │  ║
║   │  redisTokenRepository.Revoke:                                 │   │ INSERT    │  ║
║   │   ① source.Revoke → Postgres                                  │   │ ON CONFL. │  ║
║   │      INSERT revoked_tokens ON CONFLICT DO NOTHING             │   │ DO NOTHING│  ║
║   │      hata ──► 500  ve  AKIŞ DURUR                             │   └───────────┘  ║
║   │   ② ttl := time.Until(exp) ; ttl<=0 ise Redis'e yazma         │   ┌───────────┐  ║
║   │   ③ rdb.Set("jti:<jti>", 1, ttl)                          ╌╌╌╌┼╌╌►│   REDIS   │  ║
║   │      hata → NÖBETÇİYİ SİL + logla, hata DÖNDÜRME              │   │ SET + TTL │  ║
║   └───────────────────────────────┬───────────────────────────────┘   └───────────┘  ║
║                                   │                                                  ║
║   ┌───────────────────────────────▼───────────────────────────────┐   ┌───────────┐  ║
║   │ [B] raw := auth.RefreshTokenFromRequest(c)   REFRESH      [4] │╌╌►│ POSTGRES  │  ║
║   │     raw == "" → BU ADIM ATLANIR (hata değil)                  │   │ UPDATE    │  ║
║   │     refresh.Revoke(ctx, HashRefreshToken(raw), now)           │   └───────────┘  ║
║   │       UPDATE refresh_tokens SET revoked_at = now              │                  ║
║   │       WHERE token_hash=? AND revoked_at IS NULL               │                  ║
║   │     hata → SADECE LOG, akış devam eder                    [5] │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
║                                   │                                                  ║
║   ┌───────────────────────────────▼───────────────────────────────┐                  ║
║   │ [C] auth.ClearRefreshCookie(c)               COOKIE       [6] │                  ║
║   │     SetCookie(name, "", maxAge=-1, Path=/auth, Domain,        │                  ║
║   │               Secure, HttpOnly)                               │                  ║
║   │     nitelikler BİREBİR aynı olmak zorunda                     │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
╚═══════════════════════════════════╪══════════════════════════════════════════════════╝
                                    │
╔═══════════════════════════════════▼══════════════════════════════════════════════════╗
║ CEVAP                                                                                ║
║   200   { "message": "Logged out" }                                                  ║
║         Set-Cookie: refresh_token=; Max-Age=0                                        ║
╚══════════════════════════════════════════════════════════════════════════════════════╝
```

**[1]** Logout `authorized` grubunda olmak **zorunda**: hangi token'ı iptal
edeceğini bilmek için geçerli bir access token'ı çözmüş olman gerekiyor.

**[2]** Ve rota neden `/auth/logout`? Refresh cookie'nin `Path`'i `/auth` — tarayıcı
onu yalnızca `/auth/*` yollarına gönderir. Rota `/logout` olsaydı cookie **hiç
gelmezdi**, `raw` boş kalırdı, refresh token iptal edilemezdi. **Rotanın yeri
estetik değil, teknik zorunluluk.**

Denylist kaydının TTL'i `time.Until(exp)` — token'ın kendi ölüm tarihine kadar.
Daha uzun tutmak anlamsız (JWT doğrulaması `exp`'e bakıp zaten reddeder), daha
kısa tutmak açık bırakır. Denylist'in küçük kalmasının ve Redis'te **tam kopya**
tutulabilmesinin sebebi bu.

**[3]** Sıra: önce kalıcı kaynak. Postgres'e yazılamadıysa iptal gerçekleşmemiştir
→ 500. Redis'e yazılamadıysa iptal *gerçekleşti* ama kopya eksik kaldı → nöbetçi
silinir, okumalar Postgres'e düşer, kullanıcıya hata dönmez. Bir sonraki warm-up
turu kopyayı tamamlayıp nöbetçiyi geri yazar — sistem kendini onarır.

**[4]** Cookie yoksa adım atlanır ve istek yine `200` döner. Meşru sebepleri var:
başka cihazdan zaten çıkılmış, cookie'nin süresi dolmuş, tarayıcı temizlenmiş.

**[5]** Hata politikası asimetrik ve bilinçli:

```
[A] access iptal   hata → 500, AKIŞ DURUR   bu başarısızsa çıkış GERÇEKLEŞMEDİ
[B] refresh iptal  hata → log, devam        access zaten iptal, kullanıcı fiilen çıkmış
[C] cookie temizle her durumda çalışır      yerel işlem
```

En kritik olan önce, ve yalnızca o akışı durdurabiliyor.

**[6]** Cookie silmenin kuralı: `maxAge=-1` ve boş değer, ama `Path`, `Domain`,
`Secure`, `HttpOnly` birebir aynı olmalı. Tarayıcı için cookie kimliği bu
niteliklerin bileşimi — biri farklı olursa yeni bir cookie oluşturur ve orijinal
hayatta kalır. Kod aynı yardımcıları tekrar kullandığı için bu hataya düşmüyor.

**Üçü neden birden gerekli:**

```
sadece cookie silinse   → değeri kopyalayan biri kullanmaya devam eder
sadece refresh iptal    → access token 15 dk daha çalışır
sadece access iptal     → refresh ile hemen yeni access alınır
```

**Ve bir kontrast:** logout **oturum başına**, hesap başına değil. Diğer
cihazlardaki oturumlar etkilenmiyor — telefondan çıkmak dizüstünü kapatmamalı.
Sızıntı tespitinde ise `RevokeAllForUser` çağrılıyor, yani hepsi kapanıyor.
Fark, niyetin bilinip bilinmemesi.

---

## 6 — POST /chat

Projenin en uzun akışı. AI çağrısı, beyaz liste ve üç risk ailesi burada.

Kritik nokta baştan: **AI yedinci adımda.** Önünde üç middleware, iki girdi
kontrolü ve iki veritabanı sorgusu var.

```
╔══════════════════════════════════════════════════════════════════════════════════════╗
║ İSTEK                                                                                ║
║   POST /chat        { "text": "kahveye 50 lira", "account_id": 3 }                   ║
╚══════════════════════════════════════╤═══════════════════════════════════════════════╝
                                       │
╔══════════════════════════════════════▼═══════════════════════════════════════════════╗
║ MIDDLEWARE ZİNCİRİ                                                                   ║
║                                                                                      ║
║   ┌───────────────────────────────────────────────────────────────┐                  ║
║   │ RequestLogger  ·  gin.Recovery                                │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
║   ┌───────────────────────────────▼───────────────────────────────┐   ┌───────────┐  ║
║   │ AuthMiddleware(tokenRepo)                                     │╌╌►│REDIS / PG │  ║
║   │ header / format / ValidateToken / IsRevoked  ──► 401 · 500    │   │ IsRevoked │  ║
║   │ c.Set: user_id · role · jti · token_exp                       │   └───────────┘  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
║   ┌───────────────────────────────▼───────────────────────────────┐   ┌───────────┐  ║
║   │ LimitByPlan(chatLimiter, KeyByUser, chatPlanFn, …)        [1] │╌╌►│ POSTGRES  │  ║
║   │  ① chatPlanFn → userRepo.GetByID(userID)                      │   │ SELECT    │  ║
║   │     planı HER İSTEKTE taze oku                                │   │ (plan)    │  ║
║   │     hata → defaultLimit (free — EN KISITLAYICI)               │   └───────────┘  ║
║   │  ② limits[plan] → free:5 / pro:30                             │   ┌───────────┐  ║
║   │  ③ AllowWithLimit(KeyByUser(c), limit)                    ╌╌╌╌┼╌╌►│   REDIS   │  ║
║   │     INCR rl:chat:user:<id>:<dakika> + EXPIRE                  │   │ INCR+EXP  │  ║
║   │                                              aşıldı ──► 429   │   └───────────┘  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
╚═══════════════════════════════════╪══════════════════════════════════════════════════╝
                                    │
╔═══════════════════════════════════▼══════════════════════════════════════════════════╗
║ HANDLER — chatHandler.Chat                                                           ║
║                                                                                      ║
║   ┌───────────────────────────────────────────────────────────────┐                  ║
║   │ h.service == nil ?          GROQ_API_KEY yoksa ──► 503        │                  ║
║   ├───────────────────────────────────────────────────────────────┤                  ║
║   │ ShouldBindJSON(&body)   text required, max=500                │                  ║
║   │                                            geçersiz ──► 400   │                  ║
║   ├───────────────────────────────────────────────────────────────┤                  ║
║   │ userID := c.MustGet("user_id")   ·   role := c.MustGet("role")│                  ║
║   │ ChatRequest{UserID, Role, DefaultAccountID, Text}         [2] │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
╚═══════════════════════════════════╪══════════════════════════════════════════════════╝
                                    │ [chat.ChatRequest]
╔═══════════════════════════════════▼══════════════════════════════════════════════════╗
║ SERVİS — ActionService.Chat                                                          ║
║                                                                                      ║
║   ┌───────────────────────────────────────────────────────────────┐                  ║
║   │ len(runes) == 0 ?                        ──► ErrEmptyText     │                  ║
║   │ len(runes) > 500 ?                       ──► ErrTextTooLong   │                  ║
║   │            ↑ AI'DAN ÖNCE — boşuna LLM token'ı harcanmaz  [3]  │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
║   ┌───────────────────────────────▼───────────────────────────────┐   ┌───────────┐  ║
║   │ today := startOfDay(now)                                      │╌╌►│ POSTGRES  │  ║
║   │ categories.GetForUser(userID)   ← kullanıcının kendi + global │   │ 2× SELECT │  ║
║   │ accounts.ListForUser(userID)                                  │   └───────────┘  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
╚═══════════════════════════════════╪══════════════════════════════════════════════════╝
                                    │ [Text, Categories, Accounts, Today]
╔═══════════════════════════════════▼══════════════════════════════════════════════════╗
║ AI KATMANI — parser.Parse                                                       [4]  ║
║                                                                                      ║
║   ┌───────────────────────────────────────────────────────────────┐   ┌───────────┐  ║
║   │ parser.go — SÖZLEŞME (sağlayıcıdan bağımsız)                  │   │           │  ║
║   │  systemPrompt()      izinli niyetler + risk kademeleri        │   │           │  ║
║   │  outputSchema()      modelin uyacağı JSON şeması              │   │  GROQ     │  ║
║   │  buildUserPrompt()   metin + KULLANICININ kategorileri,       │╌╌►│  API      │  ║
║   │                      hesapları, bugünün tarihi           [5]  │   │           │  ║
║   │  weekdayTR()         "geçen salı"yı çözebilmesi için          │   │ HTTP POST │  ║
║   ├───────────────────────────────────────────────────────────────┤   │ chat/     │  ║
║   │ groq.go — TAŞIYICI (değiştirilebilir)                         │   │ completions│ ║
║   │  parseOnce()         HTTP POST, cevabı çöz                    │   │           │  ║
║   │  429 → retryAfterFrom() ile bekle, yeniden dene               │   └───────────┘  ║
║   │  stripCodeFence()    model JSON'u ```json ile sarmışsa temizle│                  ║
║   │  truncateForError()  hata mesajına tüm çıktıyı basma          │                  ║
║   │                                     hata ──► "parsing failed" │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
╚═══════════════════════════════════╪══════════════════════════════════════════════════╝
                                    │ [[]models.ParsedAction]  ← N tane olabilir  [6]
╔═══════════════════════════════════▼══════════════════════════════════════════════════╗
║ KARAR — handle(), her eylem için ayrı ayrı                                           ║
║                                                                                      ║
║   ┌───────────────────────────────────────────────────────────────┐                  ║
║   │ models.RiskOf(a.Intent)                    BEYAZ LİSTE    [7] │                  ║
║   │ haritada yoksa → Result{Error: "unknown or not-allowed"}      │                  ║
║   │                  payload YOK · token YOK · DÖNER              │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
║              ┌────────────────────┼────────────────────┐                             ║
║              │                    │                    │                             ║
║   ┌──────────▼─────────┐ ┌────────▼──────────┐ ┌───────▼────────────┐                ║
║   │ OKUMA              │ │ OLUŞTURMA         │ │ YIKICI             │                ║
║   │ RiskRead           │ │ RiskCreate        │ │ RiskDestructive    │                ║
║   ├────────────────────┤ ├───────────────────┤ ├────────────────────┤                ║
║   │ doğrudan çalıştır  │ │ TASLAK üret       │ │ TOKEN üret         │                ║
║   │                    │ │ YAZMA             │ │ YAPMA              │                ║
║   │ resolveAccount     │ │ buildTransaction  │ │ prepare*Action     │                ║
║   │ resolveTransaction │ │  ├ resolve*       │ │  ├ hedefi çöz      │                ║
║   │  ↑ SAHİPLİK   [8]  │ │  │  ↑ SAHİPLİK    │ │  │  ↑ SAHİPLİK     │                ║
║   │ txs.ListByAccount  │ │  ├ tutar 0?       │ │  ├ yapılabilir mi? │                ║
║   │ BuildBudgetView    │ │  │   → reddet     │ │  │  hayır→Error,   │                ║
║   │  ├ offset sınırı   │ │  ├ kategori yok?  │ │  │  TOKEN YOK      │                ║
║   │  └ HTTP ile ORTAK  │ │  │   → NeedsInput │ │  └ attachConfirm.  │                ║
║   │                    │ │  └ tarih bozuk?   │ │     newToken()     │                ║
║   │                    │ │      → düzelt+uyar│ │     pending.Create │                ║
║   │  ▼ res.Data        │ │  ▼ res.Payload    │ │  ▼ res.Token       │                ║
║   └──────────┬─────────┘ └────────┬──────────┘ └───────┬────────────┘                ║
║              │                    │                    │                             ║
║              │              ╌╌╌╌╌╌┼╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌┼╌╌╌► POSTGRES                ║
║              │              (resolve: SELECT)   (pending: INSERT)                    ║
║              └────────────────────┼────────────────────┘                             ║
╚═══════════════════════════════════╪══════════════════════════════════════════════════╝
                                    │ [[]Result]
╔═══════════════════════════════════▼══════════════════════════════════════════════════╗
║ CEVAP                                                                                ║
║   Chat() seviyesinde hata varsa → respondChatError                              [9]  ║
║     ErrEmptyText / ErrTextTooLong ──► 400 (sebebiyle)                                ║
║     diğer her şey                 ──► 503 + log (detay client'a GİTMEZ)              ║
║                                                                                      ║
║   yoksa → 200   { "results": [ … ] }                                                 ║
║     handle() içindeki hatalar isteği DÜŞÜRMEZ — res.Error'a yazılır, kod 200 kalır   ║
╚══════════════════════════════════════════════════════════════════════════════════════╝
```

**[1]** Plan **her istekte** DB'den taze okunuyor. JWT'ye gömülseydi, planını
yükselten kullanıcı token'ı yenilenene kadar eski limitte kalırdı. Bedeli: her
`/chat` bir kullanıcı sorgusu daha. Kabul edilebilir, çünkü istek zaten
kategori/hesap için veritabanına gidiyor.

Hata durumunda `defaultLimit` (free) uygulanıyor — bir DB arızasında "cömert
davran" değil "tutucu davran". Bu limitin işi maliyeti korumak; arızada maliyeti
serbest bırakmak yanlış taraf olurdu.

**[2]** `UserID` ve `Role` context'ten, `DefaultAccountID` istekten. **Hiçbiri
modelden gelmiyor** — model yalnızca `Text`'i etkiliyor.

**[3]** Girdi sınırı AI'dan **önce**. `TestChat_TooLongText_RejectedBeforeAICall`
bunu sınıyor: `f.parser.calls != 0` olursa test kırılır. Uzun metin = boşuna
LLM token'ı = boşuna para.

**[4]** Sınır şurada: `parser.go` **ne soruyoruz** (sözleşme, prompt, şema —
sağlayıcıdan bağımsız), `groq.go` **kime ve nasıl soruyoruz** (HTTP, OpenAI
formatı, yeniden deneme). Groq'u bırakıp Ollama'ya geçsen `parser.go` hiç
değişmez.

`stripCodeFence` en dürüst fonksiyon: JSON istiyorsun, model markdown gönderiyor.

**[5]** Prompt **kişiselleştirilmiş**. Kullanıcının kendi kategorileri ve
hesapları prompt'a konuyor — model "Yeme"yi genel bilgiyle tahmin etmiyor, ona
senin sözlüğün veriliyor. `Today` da aynı sebeple.

**[6]** Bir istek **N sonuç** üretebilir. Model bir cümleden birden fazla eylem
çıkarabilir; her biri ayrı `handle()`'dan geçer.

**[7]** `Parse` sonucu `[]models.ParsedAction` — model çıktısı tip sistemimize
girdi. Ama `ParsedAction{Intent: "drop_all_tables"}` kusursuz geçerli bir Go
değeri. **Tip güvenliği anlamsal güvenlik değildir.** Beyaz liste tam burada
duruyor.

**[8]** Sahiplik `resolve*` fonksiyonlarında uygulanıyor: yalnızca kullanıcının
kendi kayıtları aranıyor, başkasının kaydı "bulunamadı" olarak dönüyor.
`/transactions`'taki eksik kontrolle karşılaştır (bkz. bölüm 3, madde [4]) —
burada daha sıkı.

**[9]** Kısmi hata isteği düşürmüyor: model üç eylem döndürüp biri geçersizse,
üç `Result` döner, biri `Error` taşır, HTTP kodu yine 200. Bir cümledeki bir
isteğin başarısızlığı diğer ikisini iptal etmemeli.

`503`, `500` değil: 500 "uygulamam bozuk", 503 "dış bağımlılığım şu an yok, sonra
dene". Detay log'a gidiyor — hata metninde prompt parçası veya API anahtarı izi
olabilir.

**Risk kademesi çıktının şeklini belirliyor:**

```
RiskRead         →  res.Data      "işte veri"
RiskCreate       →  res.Payload   "işte taslak, sen gönder"
RiskDestructive  →  res.Token     "emin misin? bu fişle gel"
```

---

## 7 — POST /actions/confirm

`/chat`'in ikinci yarısı. **AI çağrılmaz** — saklanmış bir karar oynatılır.

> `/chat` düşünür, `confirm` uygular.

```
╔══════════════════════════════════════════════════════════════════════════════════════╗
║ İSTEK                                                                                ║
║   POST /actions/confirm       { "token": "act_..." }                                 ║
╚══════════════════════════════════════╤═══════════════════════════════════════════════╝
                                       │
╔══════════════════════════════════════▼═══════════════════════════════════════════════╗
║ MIDDLEWARE ZİNCİRİ                                     rate limiter YOK         [1]  ║
║   RequestLogger · gin.Recovery                                                       ║
║   AuthMiddleware(tokenRepo)          ──► 401 · 500                                   ║
║   c.Set: user_id · role · jti · token_exp                                            ║
╚══════════════════════════════════════╤═══════════════════════════════════════════════╝
                                       │
╔══════════════════════════════════════▼═══════════════════════════════════════════════╗
║ HANDLER — chatHandler.Confirm                                                        ║
║   h.service == nil ?                              ──► 503                            ║
║   ShouldBindJSON(&body)   token required, max=64  ──► 400                            ║
║   userID := c.MustGet("user_id")   ← gövdeden DEĞİL                                  ║
╚══════════════════════════════════════╤═══════════════════════════════════════════════╝
                                       │ [userID, token]
╔══════════════════════════════════════▼═══════════════════════════════════════════════╗
║ SERVİS — ActionService.Confirm                                                       ║
║                                                                                      ║
║   ┌───────────────────────────────────────────────────────────────┐   ┌───────────┐  ║
║   │ pending.Claim(ctx, userID, token, now)   TEK ATOMİK UPDATE[2] │╌╌►│ POSTGRES  │  ║
║   │ UPDATE pending_actions SET used_at = now                      │   │ UPDATE    │  ║
║   │ WHERE token=?  AND user_id=?                                  │   │           │  ║
║   │       AND used_at IS NULL  AND expires_at > ?                 │   └───────────┘  ║
║   │                                                               │                  ║
║   │ RowsAffected == 0 → ErrPendingActionInvalid              [3]  │                  ║
║   │   yok / başkasının / kullanılmış / süresi dolmuş — AYNI hata  │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
║                                   │ [*models.PendingAction]                          ║
║   ┌───────────────────────────────▼───────────────────────────────┐                  ║
║   │ switch action.Intent            8 yıkıcı niyet                │                  ║
║   │ delete/update: category · account · transaction               │                  ║
║   │ budget_delete · budget_update                                 │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
║                                   │  örnek dal: confirmDeleteCategory                ║
║   ┌───────────────────────────────▼───────────────────────────────┐   ┌───────────┐  ║
║   │ ownedCategory(userID, a.TargetID)                         [4] │╌╌►│ POSTGRES  │  ║
║   │   categories.GetByID       → ErrCategoryNotFound ──► 404      │   │ SELECT    │  ║
║   │   cat.UserID == nil ?      → ErrGlobalCategory   ──► 403      │   └───────────┘  ║
║   │   *cat.UserID != userID ?  → ErrCategoryNotFound ──► 404      │                  ║
║   └───────────────────────────────┬───────────────────────────────┘                  ║
║   ┌───────────────────────────────▼───────────────────────────────┐   ┌───────────┐  ║
║   │ txs.CountByCategory(cat.ID)        TOCTOU YENİDEN KONTROL [5] │╌╌►│ POSTGRES  │  ║
║   │ used > 0 ? → ErrCategoryInUse                    ──► 409      │   │ COUNT     │  ║
║   └───────────────────────────────┬───────────────────────────────┘   └───────────┘  ║
║   ┌───────────────────────────────▼───────────────────────────────┐   ┌───────────┐  ║
║   │ categories.Delete(cat.ID)                 YAZMA BURADA    [6] │╌╌►│ POSTGRES  │  ║
║   └───────────────────────────────┬───────────────────────────────┘   │ DELETE    │  ║
╚═══════════════════════════════════╪══════════════════════════════════ └───────────┘ ═╝
                                    │ [message]
╔═══════════════════════════════════▼══════════════════════════════════════════════════╗
║ CEVAP                                                                                ║
║   hata → respondConfirmError                                                    [7]  ║
║     ErrPendingActionInvalid ──► 400      ErrCategoryInUse / AccountInUse ──► 409     ║
║     *NotFound (4 tip)       ──► 404      ErrGlobalCategory              ──► 403      ║
║     *chat.ValidationError   ──► 400 (sebebiyle)   ·   diğer ──► 500 + log            ║
║                                                                                      ║
║   yoksa → 200   { "message": "category \"Market\" deleted" }                         ║
╚══════════════════════════════════════════════════════════════════════════════════════╝
```

**[1]** Limiter yok, çünkü confirm AI çağırmıyor — sıradan bir REST endpoint'i
kadar pahalı (~3-4 DB sorgusu). Limiter'lar endpoint'e göre değil **maliyete** göre
konmuş: para harcayan (`/chat`) ve CPU yakan (`/login`).

Bilinen açık: kimliği doğrulanmış bir kullanıcı bunu — ve diğer tüm REST
endpoint'lerini — spam'leyebilir. **Reddedilen istek bile en az bir DB sorgusuna
mal olur.** Endpoint bazında çözülmez; doğru yer nginx katmanı (`limit_req`).

**[2]** `SELECT`-sonra-`UPDATE` olsaydı iki eşzamanlı istek de geçer ve silme iki
kez çalışırdı. `used_at IS NULL` koşulunu yalnızca bir istek sağlayabilir.
"Tek kullanımlık" bir kontrol değil, bir **yarış koşulu garantisi**.

**[3]** Dört farklı başarısızlık, tek hata. Ayırsaydık başkasının token'ını
deneyen biri "bu token var ama süresi dolmuş" bilgisini elde ederdi.

`Consume` (bölüm 4) ise ayırt **ediyor** — çünkü orada "tekrar kullanım" farklı
bir güvenlik tepkisi tetikliyor. İkisi tutarsız değil: teşhis, sunucunun ne
yapacağını belirlemek gerekiyorsa yapılır; client'a giden cevap her iki durumda
da aynı kalır.

**[4]** Sahiplik **iki kez, iki katmanda**: `Claim`'in `WHERE user_id=?`'si
token'ın senin olduğunu, `ownedCategory` hedefin senin olduğunu doğruluyor. Farklı
sorular — geçerli token'ın artık senin olmayan bir hedefi gösteriyor olabilir.

**403 / 404 asimetrisi:** global kategori 403 (varlığı sır değil, herkes
`GET /categories`'te görüyor), başkasının kategorisi 404 (varlığı sır). Kural
ezber değil — her seferinde *"bu şeyin varlığı gizli mi?"* diye sorulup
uygulanıyor.

**[5]** `prepare*Action` token'ı verirken zaten bakmıştı. Yine bakılıyor, çünkü
token üretildikten sonra dünya değişmiş olabilir. `TestConfirm_TargetBecameInUse_Blocked`
bunu sınıyor.

**[6]** Yıkıcı işlemlerde yazma **burada** oluyor — `/transactions` gibi bir REST
kapısına gitmiyor. Sebep: bir silmenin "taslağı" olmaz. Risk, taslak yerine
tek kullanımlık token ile yönetiliyor.

**[7] Bulgu:** token yakma (`Claim`) ile işi yapma (`Delete`) **aynı transaction'da
değil**. `Delete` hata verirse token yanmış olur ve kullanıcı `/chat`'ten baştan
istemek zorunda kalır. Fail-safe tarafta (çift çalıştırmaktansa yakmak yeğdir)
ama nadir durumda kullanıcıyı gereksiz yere baştan başlatıyor.

---

## 8 — Arka plan işleri

Hiçbir istek tetiklemez. Dört goroutine, hepsi aynı iskelette:

```
for {
    select {
    case <-stop:      return
    case <-ticker.C:  ...iş...
    }
}
```

```
╔══════════════════════════════════════════════════════════════════════════════════════╗
║ PAYLAŞILAN DURUMA DOKUNANLAR                                    lider kilidi GEREKLİ ║
║                                                                                      ║
║   ┌───────────────────────────────────────────────────────────────┐   ┌───────────┐  ║
║   │ StartWarmUpLoop                              her 5 dakika [1] │╌╌►│   REDIS   │  ║
║   │ ctx = WithTimeout(Background, 10sn)  ← her turda YENİ         │   │ SetNX     │  ║
║   │                                                               │   │ (kilit)   │  ║
║   │ runWarmUpIfLeader:                                            │   └───────────┘  ║
║   │   SetNX("denylist:warmup:lock", 1, ttl=5dk)                   │                  ║
║   │     hata      → logla, turu atla                              │                  ║
║   │     !acquired → SESSİZCE atla (başkası yapıyor)               │                  ║
║   │     acquired  → WarmUpDenylist:                               │   ┌───────────┐  ║
║   │        ListActive(now)                                    ╌╌╌╌┼╌╌►│ POSTGRES  │  ║
║   │          WHERE expires_at > now                               │   │ SELECT    │  ║
║   │        pipeline: N × Set("jti:<jti>", 1, ttl)             ╌╌╌╌┼╌╌►│   REDIS   │  ║
║   │        pipeline: Set("denylist:warm", 1, TTL YOK)  ← EN SON   │   │ N × SET   │  ║
║   │                                                          [2]  │   └───────────┘  ║
║   │ durdurma: <-sweeperStop                                       │                  ║
║   └───────────────────────────────────────────────────────────────┘                  ║
║                                                                                      ║
║   ┌───────────────────────────────────────────────────────────────┐   ┌───────────┐  ║
║   │ cleaner.Start                                  her 1 saat [3] │╌╌►│   REDIS   │  ║
║   │ İLK TUR HEMEN çalışır  ← kapalıyken birikenler beklemesin     │   │ SetNX     │  ║
║   │                                                               │   └───────────┘  ║
║   │ runIfLeader:                                                  │                  ║
║   │   rdb == nil ? → koşulsuz çalıştır (tek kopya varsayımı)      │                  ║
║   │   SetNX("cleaner:lock", 1, ttl=1saat)                         │                  ║
║   │     !acquired → atla                                          │   ┌───────────┐  ║
║   │     acquired  → RunOnce:                                  ╌╌╌╌┼╌╌►│ POSTGRES  │  ║
║   │        tokens.DeleteExpired    ─┐                             │   │ 3× DELETE │  ║
║   │        pending.DeleteExpired    ├ biri hata verse DİĞERLERİ   │   └───────────┘  ║
║   │        refresh.DeleteExpired   ─┘ DEVAM EDER             [4]  │                  ║
║   │   logRun: hiçbir şey silinmediyse LOGLAMA                [5]  │                  ║
║   │ durdurma: <-cleanupCtx.Done()                                 │                  ║
║   └───────────────────────────────────────────────────────────────┘                  ║
╚══════════════════════════════════════════════════════════════════════════════════════╝

╔══════════════════════════════════════════════════════════════════════════════════════╗
║ SÜREÇ-YEREL DURUMA DOKUNANLAR                          kilit YANLIŞ OLURDU      [6]  ║
║                                                                                      ║
║   ┌───────────────────────────────────────────────────────────────┐                  ║
║   │ authMem.StartSweeper                        her 10 dakika     │                  ║
║   │ Sweep(now): visitors map'inde lastSeen > 10dk olanları sil    │                  ║
║   │ AĞ YOK · kilit YOK · her kopya KENDİ haritasını temizler      │                  ║
║   │ durdurma: <-sweeperStop                                       │                  ║
║   ├───────────────────────────────────────────────────────────────┤                  ║
║   │ chatMem.StartSweeper                        her 10 dakika     │                  ║
║   │ (aynısı)                                                      │                  ║
║   └───────────────────────────────────────────────────────────────┘                  ║
╚══════════════════════════════════════════════════════════════════════════════════════╝
```

**[1]** Nöbetçi yalnızca **uygulama açılışında** yazılıyordu. Redis uygulama
ayaktayken kendi başına restart olursa (bakım, OOM) veri gider ama uygulama bunu
asla öğrenmez — nöbetçi bir daha yazılmadığı için okumalar **süresiz** olarak
Postgres'e düşerdi. Bu döngü düşme süresini interval ile sınırlıyor.

Döngü içinde `defer cancel()` **yazılmıyor** — fonksiyon hiç bitmediği için hiçbir
`cancel` çalışmaz ve her turda bir context sızardı.

**[2]** Nöbetçi **en sonda**. Redis pipeline'ı atomik değil; ortada kesilirse
nöbetçi hiç yazılmaz → kopya eksik sayılır → okumalar Postgres'e düşer. Kesilme
her zaman güvenli tarafa düşüyor. Ve nöbetçinin TTL'i **yok** — "kopya geçerli"
bilgisi kendi kendine bayatlamamalı.

**Kilit iş bitince BIRAKILMIYOR**, TTL ile düşmesi bekleniyor. Bıraksaydın aynı
pencerede başka bir kopya aynı işi tekrar yapardı. Kilit burada karşılıklı
dışlama için değil, **turda-bir-kez** için.

**[3]** `Cleaner` hemen bir tur çalışıyor çünkü ön turu yok; warm-up döngüsü
çalışmıyor çünkü `main.go` 75. satırda senkron bir tur zaten yaptı.

**[4]** Bakım işi **kısmi başarıyla da değerlidir**. Bir tablo hata verdiğinde
diğerlerini iptal etmenin faydası yok.

**[5]** Boş turlar loglanmıyor. Saatte bir "0 kayıt silindi" satırı aylar içinde
log'u kirletir ve gerçek olayları gizler. `dbLogger()`'ın `ErrRecordNotFound`'u
susturmasıyla aynı refleks: **gürültü sinyali öldürür.**

**[6] Bu diyagramın tek asıl dersi.** Sweeper'lara kilit koymak **hata olurdu**:
her sürecin kendi `visitors` haritası var. Lider seçseydin yalnızca bir kopya
kendi haritasını temizler, diğerlerininki sonsuza kadar büyürdü.

Üç ayrı durum ve üç ayrı doğru cevap:

```
warm-up / cleaner   paylaşılan durum + ÇOK ADIMLI iş   → kilit gerekli
INCR sayacı         paylaşılan durum + TEK ATOMİK komut → kilit gereksiz
sweeper             süreç-yerel durum                   → kilit yanlış olur
```

> Atomik bir komutla ifade edilebilen şey kilit istemez. Çok adımlı bir iş kilit
> ister. Süreç-yerel bir iş her süreçte çalışmak zorundadır.
