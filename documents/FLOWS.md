# İstek Akışları

Bu dosya GoGinMoneyCopilot'ın çalışma zamanı akışlarını gösterir: bir istek
sisteme girdiğinde hangi katmanlardan, hangi sırayla geçtiği ve her adımda
hangi kararın verildiği.

Diyagramlar tek yönde okunur — yukarıdan aşağı. Kutular arasındaki oklarda
köşeli parantez içindeki değer (`[models.LoginInput]` gibi) o okta **akan
veriyi** gösterir. Sağa çıkan oklar akışı kesen hata yollarıdır. Kesik oklar
(`╌╌►`) süreç dışına, yani ağ üzerinden bir dış sisteme gidildiği anlamına
gelir.

> Monospace bir fontla ve satır kaydırma kapalı görüntüleyin.

## İçindekiler

| # | akış | kapsam |
|---|---|---|
| [0](#0--genel-topoloji) | Genel topoloji | istemci → nginx → app kopyaları → dış sistemler |
| [1](#1--post-login) | `POST /login` | kimlik doğrulama, token çifti üretimi |
| [2](#2--post-authrefresh) | `POST /auth/refresh` | rotation, sızıntı tespiti |
| [3](#3--post-authlogout) | `POST /auth/logout` | üç ayrı iptal |
| [4](#4--post-transactions) | `POST /transactions` | CRUD temsilcisi — 16 endpoint aynı kalıp |
| [5](#5--post-chat) | `POST /chat` | AI katmanı, beyaz liste, üç risk ailesi |
| [6](#6--post-actionsconfirm) | `POST /actions/confirm` | onay token'ı, TOCTOU |
| [7](#7--kurulum-ve-kapanış) | Kurulum ve kapanış | `main()` → sinyal → graceful shutdown |
| [8](#8--arka-plan-işleri) | Arka plan işleri | periyodik görevler, lider kilitleri |

---

## 0 — Genel topoloji

Aşağıdaki endpoint diyagramları bu topolojiyi varsayar ve tekrar çizmez.

```
                            ┌────────────────────┐
                            │      İSTEMCİ       │
                            │ tarayıcı / Postman │
                            └─────────┬──────────┘
                                      │ :8080
             ┌────────────────────────▼────────────────────────┐
             │ nginx                                           │
             │ yayınlanan TEK port — app hiçbir port açmaz     │
             │ resolver 127.0.0.11 valid=10s                   │
             │ set $upstream "app:8080"   ← DNS önbelleği tuzağı│
             │ X-Forwarded-For = $remote_addr                  │
             │   ($proxy_add_x_forwarded_for DEĞİL — sahteciliği│
             │    engellemek için istemcinin değeri EZİLİR)    │
             └────────────────────────┬────────────────────────┘
                                      │ docker ağı
              ┌───────────────────────┼───────────────────────┐
              │                       │                       │
     ┌────────▼────────┐     ┌────────▼────────┐     ┌────────▼────────┐
     │  app kopya 1    │     │  app kopya 2    │     │  app kopya N    │
     │  TRUSTED_PROXIES│     │                 │     │                 │
     │  =172.16.0.0/12 │     │                 │     │                 │
     └────────┬────────┘     └────────┬────────┘     └────────┬────────┘
              └───────────────────────┼───────────────────────┘
                   ┌───────────────────┼───────────────────┐
                   │                   │                   │
          ┌────────▼───────┐  ┌────────▼───────┐  ┌────────▼────────┐
          │   POSTGRES     │  │     REDIS      │  │   GROQ API      │
          │                │  │                │  │   (dış servis)  │
          │ kalıcı kaynak  │  │ denylist kopyası│ │ LLM çağrıları   │
          │ 9 tablo        │  │ rate limit sayacı│ │ OpenAI-uyumlu  │
          │                │  │ lider kilitleri │  │ base URL        │
          └────────────────┘  └────────────────┘  └─────────────────┘
```

**Neden nginx var:** app konteyneri port yayınlamıyor, bu yüzden birden fazla
kopya çalıştırılabiliyor. Tek port'u nginx sahipleniyor ve gelen isteği
kopyalara dağıtıyor.

**Neden `$remote_addr`:** `$proxy_add_x_forwarded_for` kullanılsaydı istemcinin
gönderdiği sahte değer listenin başında kalırdı ve IP bazlı hız sınırı
atlatılabilirdi. Üzerine yazmak tek güvenli seçenek.

**Neden `TRUSTED_PROXIES`:** gin varsayılan olarak tüm vekillere güvenir. Vekil
arkasındayken hangi ağın güvenilir olduğu açıkça söylenmezse ya sahte başlık
kabul edilir ya da tüm kullanıcılar tek sayaca düşer.

---

## 1 — POST /login

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

## 2 — POST /auth/refresh

_(hazırlanıyor)_

---

## 3 — POST /auth/logout

_(hazırlanıyor)_

---

## 4 — POST /transactions

_(hazırlanıyor)_

---

## 5 — POST /chat

_(hazırlanıyor)_

---

## 6 — POST /actions/confirm

_(hazırlanıyor)_

---

## 7 — Kurulum ve kapanış

_(hazırlanıyor)_

---

## 8 — Arka plan işleri

_(hazırlanıyor)_
