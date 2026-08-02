# LEARNING.md — proje hakimiyeti çalışma defteri

Bu dosya dokümantasyon değil, bir **çalışma defteri**. Amaç: projeyi tecrübeli
developer'lara eksiksiz anlatabilmek — hem product hem developer tarafından.

Hedef bar: "neden böyle yaptın?" sorusuna her seferinde gerekçeyle cevap
verebilmek. "Öyle yaptık" yeterli değil.

---

## Nasıl çalışıyoruz

Her oturum aynı ritmi izler:

1. **Isınma** — önceki oturumlardan 2 soru (aralıklı tekrar)
2. **Kodu kapat, mekanizmayı anlat** — tıkandığın yer o oturumun konusudur
3. **Aç ve farkı gör** — anlattığınla gerçek arasındaki fark = öğrenilecek şey
4. **"Ne bozulur?"** — bir satır değişse neyin kırılacağını *önceden* söyle
5. **Savunma kartı** — o oturumun kararını 4 alana yaz

İş bölümü:

| birlikte | tek başına |
|---|---|
| akıl yürütme, savunma, "neden böyle" | okuma, gezinme, "nerede ne var" |

**Canlı deneme:** `GoGinMoneyCopilot.postman_collection.json` — 23 endpoint +
5 hata senaryosu. Chat klasöründeki 7 istek, Faz 1'de konuşulan davranışları
(taslak üretimi, girdi sınırı, onay token'ı, tek kullanımlık olma) canlı
gösteriyor. Bir kavram oturmadığında oku değil, **çalıştır**.

---

## Faz planı ve durum

| Faz | Konu | Durum |
|---|---|---|
| 0 | Kör harita — paket sorumlulukları | **bitti (11/11)** |
| 0.5 | Akış diyagramları (harita) | 4/8 |
| 1 | AI güven sınırı | **bitti** |
| 2 | Kimlik, oturum, iptal | **sırada** |
| 3 | Veri katmanı ve bütünlük | — |
| 4 | Ölçek, dağıtım, güvenlik | — |
| 5 | Product: kim, neden, ne kadar | — |
| 5.5 | Algoritmalar (yakınlaştırma) | — |
| 6 | Savunma provası | — |

**Faz 5.5 — yakınlaştırılacak yerler.** Kodun çoğu tesisat, küçük bir kısmı
düşünce. Bu sekizi ifade ölçeğinde çizmeye/okumaya değer:

```
models.Budget.PeriodAt + floorDiv + daysBetween   takvim matematigi, negatif offset
RateLimiter.AllowWithLimit                        jeton kovasi
pending.Claim                                     tek atomik UPDATE
refreshRepo rotation + reuse detection            sizinti tespiti
redisTokenRepository warm-up + nobetci anahtar    veri kaybi tespiti
runIfLeader / SetNX                               lider secimi
chat.matchCategory / matchAccount                 isim eslestirme
ai.stripCodeFence / retryAfterFrom                gercek dunya ayristirma
```

**Faz 0 — kalan iş:** 5 paket için tek cümlelik sorumluluk — sırasıyla
`middleware` → `repositories` → `handlers` → `ai` → `chat`. Ezberden değil,
**açıp okuyarak**. Biten 6 paketin cümleleri aşağıdaki tabloda.

Okuma tekniği: satır satır değil — `grep -n "^func \|^type \|^// Package"` ile
iskeleti çıkar, sonra ilgini çekenin gövdesine in.

---

## Faz 0 — paket sorumlulukları

**Kural:** sorumluluk cümlesi paketin *işini* söyler, kullandığı API'yi değil.
En güçlü hâli neyi **dışladığını** da söyler. Bir paketi anlatan en güçlü tek
bilgi ise bağımlılık yönüdür.

| # | paket | sorumluluk |
|---|---|---|
| 1 | `validators` | Uygulamaya özgü doğrulama kurallarını tanımlar ve gin'in doğrulayıcısına kaydeder. Şu an tek kural: `accountname` (regex `\p{L}` — Türkçe harfler geçsin diye). |
| 2 | `database` | Dış veri depolarına (Postgres, Redis) bağlantı kurar, sağlığını başlangıçta doğrular, global tutamaçları sunar. İş mantığı içermez. |
| 3 | `maintenance` | Süresi dolmuş kayıtları üç Postgres tablosundan periyodik siler; çok kopyalı dağıtımda turu tek kopyanın üstlenmesi için Redis kilidi kullanır. Doğruluk değil, disk/indeks meselesi. |
| 4 | `src` | Composition root. Bağımlılıkları kurar ve birbirine bağlar, rotaları/middleware zincirlerini tanımlar, sunucuyu başlatır ve kapatır. Domain kuralı içermez. |
| 5 | `models` | Bütün katmanların paylaştığı veri şekillerini ve bu şekiller üzerindeki saf hesapları tanımlar — DB varlıkları, API girdi/çıktı tipleri, AI sözleşmesi. Hiçbir I/O yapmaz, hiçbir proje paketine bağlı değildir. |
| 6 | `auth` | Kimlik doğrulama ilkelerini üretir ve doğrular: access token (imzalı, durumsuz), refresh token (opak, hash'li), parola hash'i, refresh cookie nitelikleri. Hiçbir şeyi **saklamaz**. |
| 7 | `middleware` | İsteği handler'a ulaşmadan önce (ve cevap dönerken) süzen çapraz kesen kurallar: kimlik doğrulama + iptal kontrolü, hız sınırlama, istemci IP'sinin güvenilir tespiti, yapılandırılmış istek logu. İş kuralı içermez. |
| 8 | `repositories` | Veritabanına erişen **tek** katman. Her tablo için arayüz + gorm implementasyonu, sahiplik kapsamlı sorgular, Postgres hata kodlarının iş diline çevrilmesi. Arayüzler testlerdeki sahte repo'ların dikişidir. |
| 9 | `handlers` | HTTP ile domain arasındaki çeviri. Girdiyi bağlar ve şeklen doğrular, kimliği **context'ten** (gövdeden değil) alır, işi devreder, domain hatalarını durum koduna çevirir. SQL görmez. |
| 10 | `ai` | Serbest metni modele sorulacak isteğe çevirir, cevabı `models.ParsedAction`'a dönüştürür. `parser.go` sözleşme + prompt (sağlayıcıdan bağımsız), `groq.go` tek bir OpenAI-uyumlu taşıyıcı. Çıktının **geçerli** olduğunu değil, doğru **şekilde** olduğunu garanti eder. |
| 11 | `chat` | Karar katmanı: niyete izin var mı, kayıt bu kullanıcının mı, veri geçerli mi, onay gerekir mi, onay anında hâlâ geçerli mi. İki giriş kapısı: `Chat()` düşünür, `Confirm()` uygular. HTTP bilmez, SQL görmez. |

### Okurken çıkan notlar

**`models/intent.go` bir liste değil, AI ile sözleşmenin tamamı:**

| ne | nerede |
|---|---|
| model neyi *söyleyebilir* | `Intent` sabitleri — 17 tane, beyaz liste |
| söylediği ne kadar *riskli* | `Risk` + `RiskOf` — read / create / destructive |
| hangi *şekilde* söylemek zorunda | `ParsedAction`, `ActionParams` |

Chat üzerinden `login` / `logout` **yok** — bilinçli sınır.

**`models/budget.go` üç ayrı türde struct barındırıyor** (269 satır, 10 tip):

```
Budget, BudgetCategory                            -> DB varlıkları
BudgetCategoryInput, CreateBudgetInput, Update…   -> API girdileri (DTO)
BudgetView, BudgetSummaryView, PeriodView, …      -> çıktı şekilleri
Period                                            -> hesap sonucu
```

Entity + DTO + view aynı dosyada. Kusur değil, ölçeğe uygun tercih — ama
sorulabilir. Ayırmanın bedeli: neredeyse aynı iki struct + elle yazılmış
mapping kodu.

**Bağımlılık yeri:**

```
models  ->  (hiçbir proje paketi)
auth    ->  models, gin
```

"I/O yapmıyor" ile "bağımsız" aynı şey değildir.

### Katmanlamanın tek cümlesi

> **`chat` neyin olabileceğine karar verir. `handlers` bunun HTTP'de nasıl
> söyleneceğine, `repositories` nasıl saklanacağına.** Üçü üç ayrı soru, o
> yüzden üç ayrı paket.

`chat` paketinin iki giriş kapısı:

```
Chat()     -> DÜŞÜNME.  AI çağrılır, karar üretilir ve saklanır. Yazma yok.
Confirm()  -> UYGULAMA. AI yok, saklanmış karar oynatılır. Yazma burada.
```

### `ai` katmanı güven üretmez

`Parse` sonucu `[]models.ParsedAction` — model çıktısı tip sistemimize girdi.
Ama `models.ParsedAction{Intent: "drop_all_tables"}` kusursuz geçerli bir Go
değeridir. **Tip güvenliği anlamsal güvenlik değildir.** Beyaz liste ve
doğrulama bir sonraki katmanda (`chat`).

### access token vs refresh token

`auth`'ta `jwt.go` ile `refresh.go`'nun ayrı dosyalar olmasının sebebi: **iki
farklı güvenlik modeli.**

| | access token (`jwt.go`) | refresh token (`refresh.go`) |
|---|---|---|
| ne | imzalı, kendini taşıyan JWT | rastgele opak dize |
| sunucu ne saklıyor | **hiçbir şey** | SHA-256 hash'i |
| doğrulama | imza + süre — **sorgu yok** | veritabanında ara |
| iptal edilebilir mi | doğrudan **hayır** | evet |
| ömür | ~15 dk | gün/hafta |
| nerede durur | frontend belleği | HttpOnly cookie |

Tek eksen hepsini açıklıyor: **durumsuz (stateless) vs durumlu (stateful).**

- Durumsuz hızlıdır, ölçeklenir, sorgu istemez — ama geri alınamaz. Bedeli:
  kısa ömür + sonradan eklenen jti denylist yaması.
- Durumlu her kullanımda bir sorgu ister — ama iptal, rotation ve sızıntı
  tespiti mümkün olur.

İkisi "token" kelimesini paylaşıyor, başka hiçbir şeyi paylaşmıyor. Aynı
dosyaya konsalardı var olmayan bir akrabalık ima edilmiş olurdu.

---

## Akış diyagramları

Ölçek kuralı: **bir kutu = bir sorumluluk devri.** Fonksiyon içinde kalan ve
kimseye devretmeyen bir `if` kutu değil, olsa olsa çıkış okudur. (Klasik
ifade-ölçeği akış şeması ayrı bir araçtır; 20-50 satırlık tek bir algoritma
için doğrudur, katmanlı bir sistem için değil.)

### POST /actions/confirm

```
POST /actions/confirm   { "token": "act_..." }
  |
  +- RequestLogger
  +- Recovery
  +- AuthMiddleware
  |    +- header / "Bearer <x>" / ValidateToken ---- 401
  |    +- tokens.IsRevoked(jti)  [I/O] ------------- 401 / 500
  |    +- c.Set: user_id, role, jti, token_exp
  |
  +- chatHandler.Confirm
       +- h.service == nil? ------------------------ 503
       +- ShouldBindJSON --------------------------- 400
       +- userID := c.MustGet("user_id")       <- context'ten, govdeden DEGIL
       |
       +- service.Confirm(ctx, userID, token)
            |
            +- pending.Claim(...)                <- TEK ATOMIK UPDATE
            |    UPDATE pending_actions SET used_at = now
            |    WHERE token=? AND user_id=? AND used_at IS NULL AND expires_at>?
            |    RowsAffected == 0 --------------- ErrPendingActionInvalid
            |    v [*models.PendingAction]
            |
            +- switch action.Intent   (8 yikici niyet)
                 +- confirmDeleteCategory            <- ornek dal
                      +- ownedCategory(userID, TargetID)
                      |    +- categories.GetByID ------- ErrCategoryNotFound
                      |    +- cat.UserID == nil? ------- ErrGlobalCategory
                      |    +- *cat.UserID != userID? --- ErrCategoryNotFound
                      +- txs.CountByCategory ---------- TOCTOU yeniden kontrol
                      |    used > 0? ------------------ ErrCategoryInUse
                      +- categories.Delete ------------ YAZMA burada
                           v [message string]
       +- err != nil -> respondConfirmError -> 400 / 403 / 404 / 409 / 500
       +- 200  { "message": "category \"Market\" deleted" }
```

Notlar:

- **`Claim` tek atomik UPDATE.** SELECT-sonra-UPDATE olsaydı iki eşzamanlı
  istek de geçer, silme iki kez çalışırdı. `used_at IS NULL` koşulunu yalnızca
  bir istek sağlayabilir. "Tek kullanımlık" bir kontrol değil, bir **yarış
  koşulu garantisi**.
- **Dört başarısızlık, tek hata.** Yok / başkasının / kullanılmış / süresi
  dolmuş — hepsi `ErrPendingActionInvalid`. Ayırsaydık token'ın varlığı sızardı.
- **Sahiplik iki kez, iki katmanda.** `Claim`'in `WHERE user_id=?`'si token'ın
  senin olduğunu, `ownedCategory` hedefin senin olduğunu doğrular. Farklı sorular.
- **403 vs 404 asimetrisi.** Global kategori 403 (varlığı sır değil, herkes
  `GET /categories`'te görüyor); başkasının kategorisi 404 (varlığı sır).
  Kural ezber değil: her seferinde *"bu şeyin varlığı gizli mi?"* diye sorulur.
- **Bulgu:** token yakma (`Claim`) ile işi yapma (`Delete`) aynı transaction'da
  değil. `Delete` hata verirse token yanmış olur, kullanıcı bastan istemek
  zorunda kalir. Fail-safe tarafta ama bilincli mi tesadüf mü, cevabin olsun.

### POST /chat

```
POST /chat   { "text": "kahveye 50 lira", "account_id": 3 }
  |
  +- RequestLogger
  +- Recovery
  +- AuthMiddleware        (yukaridakiyle ayni)
  |
  +- LimitByPlan(chatLimiter, KeyByUser, chatPlanFn, ...)
  |    +- chatPlanFn -> userRepo.GetByID  [DB]   <- plani HER ISTEKTE taze oku
  |    |    hata -> defaultLimit (free, en kisitlayici)
  |    +- limits[plan] -> free:5 / pro:30
  |    +- AllowWithLimit(key, limit) --------------- 429 + Retry-After
  |
  +- chatHandler.Chat
       +- h.service == nil? ------------------------ 503
       +- ShouldBindJSON --------------------------- 400   [max=500 burada da]
       +- userID, role := c.MustGet(...)
       |    v [chat.ChatRequest{UserID, Role, DefaultAccountID, Text}]
       |
       +- service.Chat(ctx, req)
            +- len(runes) == 0 ? ---------------- ErrEmptyText     } AI'DAN
            +- len(runes) > 500 ? --------------- ErrTextTooLong   } ONCE
            |
            +- today := startOfDay(now)
            +- categories.GetForUser(userID)  [DB]
            +- accounts.ListForUser(userID)   [DB]
            |
            +- parser.Parse(ctx, ai.ParseInput{         <== AI CAGRISI (7. adim)
            |      Text, Categories, Accounts, Today })
            |    +- groq.go: HTTP POST, 429'da bekle-yeniden dene, stripCodeFence
            |    +- hata --------------------------- "parsing failed: %w"
            |    v [[]models.ParsedAction]   <- N tane olabilir
            |
            +- for her action: handle(...)
                 +- models.RiskOf(a.Intent)   <== BEYAZ LISTE
                 |    bulunamadi -> Result{Error}, payload yok, token yok
                 |
                 +- switch a.Intent  ---- uc aile ----
                      |
                      +- OKUMA (RiskRead) --- dogrudan calistir
                      |    resolveAccount / resolveTransaction  [sahiplik]
                      |    BuildBudgetView (offset siniri)
                      |         v res.Data
                      |
                      +- OLUSTURMA (RiskCreate) --- TASLAK uret, YAZMA
                      |    buildTransaction / buildBudget
                      |      +- resolve*  [sahiplik]
                      |      +- tutar 0? ------------ res.Error (reddet)
                      |      +- kategori yok? ------- NeedsInput + Warning
                      |      +- tarih pencere disi? - duzelt + Warning
                      |         v res.Payload
                      |
                      +- YIKICI (RiskDestructive) --- TOKEN uret, YAPMA
                           prepare*Action
                             +- hedefi coz  [sahiplik]
                             +- yapilabilir mi? hayir -> res.Error, TOKEN YOK
                             +- attachConfirmation
                                  +- newToken() + pending.Create  [DB YAZMA]
                                  v res.RequiresConfirmation, res.Token, res.Summary
       +- err != nil -> respondChatError
       |    +- ErrEmptyText / ErrTextTooLong ---- 400 (sebebiyle)
       |    +- diger her sey ------------------- 503 + log
       +- 200  { "results": [ ... ] }
```

Notlar:

- **AI yedinci adımda.** Önünde 3 middleware, 2 girdi kontrolü, 2 DB sorgusu;
  `LimitByPlan` içindeki plan okuması ile birlikte **AI'a ulaşmadan 3 kez
  veritabanına gidiliyor**.
- **Prompt kişiselleştirilmiş.** `ParseInput` kullanıcının kendi kategorilerini
  ve hesaplarını taşıyor. Model "Yeme"yi genel bilgiyle tahmin etmiyor — ona
  senin sözlüğün veriliyor. `Today` da aynı sebeple ("geçen salı" için).
- **Bir istek, N sonuç.** Model birden fazla eylem dönebilir; her biri ayrı
  `handle()`'dan geçer.
- **Kısmi hata isteği düşürmez.** `handle()` içindeki hatalar `res.Error`'a
  yazılır ve HTTP durumu yine 200 olur. Sadece `Chat()` seviyesindeki hatalar
  (boş metin, AI erişilemiyor) durum kodunu değiştirir.
- **Risk kademesi çıktının şeklini belirler:**

  ```
  RiskRead        -> res.Data      "iste veri"
  RiskCreate      -> res.Payload   "iste taslak, sen gonder"
  RiskDestructive -> res.Token     "emin misin? bu fisle gel"
  ```

- **503, 500 değil.** 500 "uygulamam bozuk", 503 "dış bağımlılığım şu an yok".
  Detay log'a gider, client'a gitmez (hata metninde prompt parçası veya API
  anahtarı izi olabilir).

### POST /auth/refresh

```
POST /auth/refresh      govde YOK, Cookie: refresh_token=<raw>
  |
  +- RequestLogger / Recovery / Limit(authLimiter, KeyByIP) ---- 429
  |
  |   AuthMiddleware YOK - buraya access token'in suresi doldugu icin gelinir
  |
  +- authHandler.Refresh
       +- auth.RefreshTokenFromRequest(c)  <- cookie'den  |  bos? --- 401
       +- auth.HashRefreshToken(raw)       <- SHA-256
       |
       +- refresh.Consume(ctx, hash, now)             <- ATOMIK UPDATE
       |    UPDATE refresh_tokens SET used_at = now
       |    WHERE token_hash=? AND used_at IS NULL
       |          AND revoked_at IS NULL AND expires_at > ?
       |    +- RowsAffected == 1 -- kaydi getir ------ OK
       |    +- RowsAffected == 0 -- NEDEN? ikinci sorgu:
       |         +- kayit yok ------------ ErrRefreshTokenInvalid  record=nil
       |         +- UsedAt != nil -------- ErrRefreshTokenReused   record=DOLU
       |         +- iptal/suresi dolmus -- ErrRefreshTokenInvalid  record=nil
       |
       +- err != nil -> handleRefreshFailure
       |    +- Reused && record != nil ?
       |    |    +- refresh.RevokeAllForUser(record.UserID)  <- TUM OTURUMLAR
       |    +- ClearRefreshCookie
       |    +- 401 "Session expired"                        <- HER DURUMDA AYNI
       |
       +- users.GetByID(record.UserID)     <- ROLU TAZE OKU
       |    hata -> ClearRefreshCookie + 401
       |
       +- issueTokenPair(c, user)
            +- auth.GenerateToken(user.ID, user.Role)  -> access token, YENI jti
            +- auth.NewRefreshToken()                  -> raw + hash
            +- refresh.Create({UserID, TokenHash})     <- ham deger ASLA yazilmaz
            +- auth.SetRefreshCookie(c, raw)
            +- 200 { "token": "<access>" }
```

Notlar:

- **`AuthMiddleware` yok.** Buraya access token öldüğü için gelinir; geçerli
  token istemek döngüsel olurdu. Kimlik cookie'nin kendisi.
- **SHA-256, bcrypt değil.** Refresh token 32 byte `crypto/rand` — tam entropi,
  tahmin edilecek bir şey yok. Bcrypt'in yavaşlığı düşük entropili parolalar
  içindir. Üstelik bu arama her yenilemede çalışıyor, hızlı olmalı.
- **`Consume` teşhis yapıyor, `Claim` yapmıyor.** Burada "tekrar kullanım" ile
  "geçersiz" farklı güvenlik tepkisi gerektiriyor (biri tüm oturumları kapatıyor),
  o yüzden ikinci sorgu atılıyor. Ama client'a giden cevap yine aynı.
  **İçeride teşhis etmek ile dışarıya açıklamak farklı şeylerdir.**
- **Hatayla birlikte kayıt da dönüyor** (`return &existing, ErrRefreshTokenReused`)
  — çağıranın `UserID`'ye ihtiyacı var, yoksa kimin oturumlarını kapatacağını bilemez.
- **Sızıntıda tüm oturumlar kapanıyor.** Sadece o token'ı iptal etmek yetmez;
  saldırgan zaten en yeni token'ı almış olabilir.
- **Rol taze okunuyor.** Token'a gömülseydi yetkisi alınmış kullanıcı eski
  yetkisini token ölene kadar korurdu. `/chat`'teki plan okumasıyla aynı gerekçe.

### POST /transactions

```
POST /transactions
{ account_id, category_id, amount, type, description, transaction_date }
  |
  +- RequestLogger / Recovery
  +- AuthMiddleware  (header/format/ValidateToken/IsRevoked -> 401|500, sonra c.Set)
  |
  |   rate limiter YOK - para harcamiyor, siradan DB maliyeti
  |
  +- transactionHandler.CreateTransaction
       +- ShouldBindJSON(&input) --------------------- 400
       |     account_id required | category_id required
       |     amount required,gt=0 | type oneof(income expense)
       |     description max=100  | transaction_date required (RFC3339)
       |
       +- getAccountForRequest(c, h.accounts, input.AccountID)
       |    +- admin ise  -> accounts.GetByID(accountID)
       |    +- degilse    -> accounts.GetByIDForUser(accountID, userID)  <- SAHIPLIK
       |         bulunamadi -> ErrAccountNotFound --- 404
       |
       |   !! category_id icin SAHIPLIK KONTROLU YOK  (bkz. Bulgular #5)
       |
       +- transactions.Create(ctx, input)
       |    db.Create(&tx)
       |      FK: account_id -> accounts, category_id -> categories
       |      CHECK: type IN ('income','expense')
       |
       +- 201 { "message": "Transaction created!" }
```

Bu rota **tek yazma kapısıdır** — `/chat` işlem yazmaz, taslak üretir ve
frontend onu buraya gönderir. Faz 1'deki bütün "tek kapı" tartışmasının
fiziksel karşılığı bu 12 kutu.

Karşıtlık (sıradan CRUD ile AI yolu):

| | `/transactions` | `/chat` |
|---|---|---|
| rate limiter | yok | var, plana göre |
| AI | yok | var (7. adım) |
| çıktı | doğrudan yazar, 201 | taslak / veri / token |
| sonuç sayısı | 1 | N olabilir |
| kısmi hata | yok | 200 içinde per-result error |
| yazma | **gerçek INSERT** | sadece `pending_actions` |

### Kalan diyagramlar

| # | diyagram | neden |
|---|---|---|
| 5 | `POST /auth/logout` | tek istekte üç iptal, kısa |
| 6 | `POST /login` | ezberden tekrar |
| 7 | kurulum + kapanış | `main()` → ... → graceful shutdown (hiçbir istek tetiklemez) |
| 8 | arka plan işleri | Cleaner + denylist warm-up, ikisi de lider kilidiyle |

İstek akışları **çalışma zamanını** kapsar; 7 ve 8 olmadan ~800 satır
(main.go, maintenance, warm-up döngüsü) hiçbir diyagramda görünmez.

---
## Faz 1 — çıkarımlar

Ezberlenecek cümleler. Tecrübeli developer'la konuşurken kuracağın cümleler
bunlar.

### 1. Doğrulamanın sınırı

> Kod **yapısal** doğruluğu doğrulayabilir, **anlamsal** doğruluğu
> doğrulayamaz. Anlamı yalnızca kullanıcı doğrulayabilir. O yüzden modelin
> ürettiği her şey bir *öneri*dir, *karar* değil.

`amount: 5000` ile `amount: 50` kod açısından ayırt edilemez — ikisi de pozitif,
ikisi de geçerli kategoriye bağlı, ikisi de kullanıcının hesabında. Hiçbir
doğrulama kuralı "bu, kahve için fazla" diyemez.

### 2. Tek yazma kapısı (single point of enforcement)

Bugün veritabanına işlem yazan **tek yer** var: `POST /transactions` handler'ı.
Chat katmanı yazmaz — o handler'a **girdi üretir** (`res.Payload`).

Chat kendi yazma yolunu kullansaydı her yeni kural iki yere yazılmak zorunda
kalırdı ve **derleyici sana ikinci yeri unuttuğunu söylemezdi**.

> İki yeri güncellemeyi *hatırlamaya* çalışmıyoruz — ikinci bir yerin var
> olmasını *imkânsız* kılıyoruz. Disiplin meselesi değil, mimari meselesi.

### 3. Ayrışma ve etki yarıçapı

**Ayrışma (drift):** aynı kuralın iki kopyasının zamanla farklılaşması.

> Ayrışma **veri bozulmasına** yol açacaksa kural tek yerde olmalı; yol
> açmayacaksa iki yerde olabilir.

| | `max=500` ayrışırsa | yazma kuralı ayrışırsa |
|---|---|---|
| kullanıcı hata görür mü | evet, her seferinde | hayır, `200 OK` |
| veriye ne olur | hiçbir şey | kalıcı olarak bozulur |
| ne zaman fark edilir | dakikalar içinde | belki hiç |

`max=500` gerçekten iki yerde: `handlers/chat_handlers.go:31` ve
`chat/service.go:73`. Sebebi kısmen Go: struct etiketleri string sabiti olmak
zorunda, `binding:"max=maxTextLength"` yazılamaz.

### 4. İki seviye koruma

| seviye | ne | derleyici durdurur mu |
|---|---|---|
| 1 — tek kapı | yeteneği var, kullanmamayı seçtik | hayır |
| 2 — dar arayüz | yeteneği hiç vermedik | evet |

Go'da **arayüzü ihtiyacı olan taraf tanımlar.** Repository'ye dokunmadan
tüketicinin gördüğü pencereyi daraltırsın. Aynı nesne, iki farklı pencere.

Projede uygulanmış örnek — `src/main.go:144`:

```go
var authLimiter middleware.Limiter     = authMem   // dar
var chatLimiter middleware.FullLimiter = chatMem   // geniş
```

`authMem` ile `chatMem` **aynı tipte** (`*RateLimiter`). `authMem`'in
`AllowWithLimit` metodu fiilen var, ama `Limiter` olarak ilan edilerek o yetenek
bilerek atıldı. Sonuç: `LimitByPlan(authLimiter, ...)` **derlenmez** — `/login`'in
plana göre limit uygulaması imkânsız, unutulduğu için değil derleyici izin
vermediği için.

### 5. Limitin yeri, koruduğun şeyin kapsamına göre seçilir

Bu projede rate limiter iki şeyden birini korur: **para** ya da **CPU**.

| endpoint | limiter | ne korunuyor |
|---|---|---|
| `/login`, `/register`, `/auth/refresh` | IP başına, sabit eşik | bcrypt CPU'su |
| `/chat` | kullanıcı başına, plana göre | LLM parası |
| diğer tüm REST endpoint'leri | yok | (sıradan DB maliyeti) |

> Maliyet endpoint'e özgüyse → endpoint'e limiter.
> Maliyet genelse (DB yükü, bant genişliği) → önde duran global katman (nginx).

Tek bir endpoint'i korumak **güvenlik tiyatrosudur** — saldırgan bir sonrakine
geçer.

### 6. Doğrulama katmanının üç davranışı

Ayrım tek soruyla belirlenir: **doğru değeri kim bilebilir?**

| kim bilebilir | davranış | örnek | test |
|---|---|---|---|
| kod | düzelt + uyar, devam et | tarih 2020 → bugün | `TestChat_StaleYear_ClampedToToday` |
| sadece kullanıcı | düşür + uyar, **sor** | kategori 999 yok | `TestChat_UnknownCategory_DroppedWithWarning` |
| kimse | reddet | tutar 0 | `TestChat_ZeroAmount_Rejected` |

Ortadaki vakada `Payload` **nil** — iş devam etmez, `NeedsInput` ile kullanıcıya
sorulur.

### 7. Ana eksen

Faz 1'in tamamı tek bir sorunun etrafında dönüyor:

> **Kod neyi bilebilir, neyi sadece kullanıcı bilebilir?**

AI'ın yazmaması, taslak deseni, onay token'ı, doğrulama katmanının üç davranışı
— hepsi bu tek sorunun farklı cevapları.

---

## Terim netliği: "token" dört farklı şey

Konuşurken hangisinden bahsettiğini **belirt**, yoksa karşı taraf kaybolur.

| terim | ne |
|---|---|
| access token | JWT, kimlik kanıtı, ~15 dk, bellekte |
| refresh token | HttpOnly cookie, oturum yenileme, DB'de hash'li |
| onay token'ı (`act_...`) | tek kullanımlık "şu silmeyi yap" fişi |
| LLM token'ı | Groq'un faturalandırma birimi |

---

## Middleware zincirleri

Gin'de **grup middleware'i rota satırında görünmez** — zinciri yukarıdan okumak
zorundasın. Güvenlik incelemesinde en sık yapılan hata budur.

```
POST /register         RequestLogger -> Recovery -> Limit(IP) -> authHandler.Register
POST /login            RequestLogger -> Recovery -> Limit(IP) -> authHandler.Login
POST /auth/refresh     RequestLogger -> Recovery -> Limit(IP) -> authHandler.Refresh

POST /chat             RequestLogger -> Recovery -> AuthMiddleware -> LimitByPlan -> chatHandler.Chat
POST /actions/confirm  RequestLogger -> Recovery -> AuthMiddleware -> chatHandler.Confirm
GET  /accounts         RequestLogger -> Recovery -> AuthMiddleware -> accountHandler...
```

`AuthMiddleware` neden `LimitByPlan`'dan **önce**:

- `chatPlanFn` içinde `c.MustGet("user_id")` var → yoksa **panic** → `Recovery`
  yakalar → her `/chat` isteği **500**
- Daha derin sebep: `LimitByPlan` kullanıcı başına ve plana göre limit uyguluyor.
  Kimliği bilmeden ikisi de anlamsız.

---

## Savunma kartları

### `/actions/confirm`'e rate limiter koymamak

```
KARAR      : /actions/confirm'e rate limiter koymadık

ALTERNATİF : /chat gibi kullanıcı başına bir limiter eklemek

NEDEN BU   : confirm AI çağırmaz — sıradan bir REST endpoint'i kadar pahalı
             (~3-4 DB sorgusu, 0 LLM çağrısı). Limiter'ları endpoint'e değil
             MALİYETE göre koyduk: para harcayan (/chat) ve CPU yakan (/login).

FEDA ETTİK : kimliği doğrulanmış bir kullanıcı confirm'ü — ve diğer tüm REST
             endpoint'lerini — spam'leyerek DB'yi yorabilir. Reddedilen istek
             bile en az bir DB sorgusuna mal olur. Endpoint bazında çözülmez,
             nginx katmanında çözülür. Bilinen ve kabul edilmiş açık.
```

### Chat'in işlem yazmaması (taslak deseni)

```
KARAR      : chat işlem oluştururken DB'ye yazmaz, res.Payload döner;
             gerçek yazma POST /transactions'tan geçer

ALTERNATİF : chat'in enjekte edilmiş txs repo'suyla doğrudan Create çağırması

NEDEN BU   : (1) modelin ürettiği tutar/kategori/tarih anlamca yanlış olabilir
             ve bunu yalnızca kullanıcı doğrulayabilir — taslak kullanıcıya
             gösterilir, onaylanır veya düzenlenir.
             (2) yazma tek kapıdan geçince doğrulama/sahiplik/iş kuralları tek
             yerde yaşar; ikinci bir yol olsaydı yeni kurallar sessizce
             ayrışırdı.

FEDA ETTİK : akış iki adımlı — frontend taslağı alıp ikinci bir istek atmak
             zorunda. Ayrıca engel yalnızca "seviye 1" (karar); "seviye 2"
             (derleyici) değil: chat'e hâlâ tam yetkili TransactionRepository
             veriliyor, Create çağırmak derlenir. Bkz. Bulgular #1.
```

---

## Bulgular — tespit edilmiş, henüz kapatılmamış

1. **`chat` paketi `Create` yeteneğini gereksiz taşıyor.**
   `chat/` altında `s.txs.Create` çağrısı yok (kontrol edildi), ama arayüzde
   duruyor. `Create` içermeyen dar bir arayüz tanımlanarak bu derleyici
   seviyesinde imkânsız kılınabilir. Repository'ye dokunmaya gerek yok.

2. **`max=500` iki yerde literal olarak duruyor.**
   `handlers/chat_handlers.go:31` ve `chat/service.go:73`. Ayrışma riski var ama
   etkisi hafif (gürültülü hata, veri bozulmaz) — bu yüzden acil değil.

3. **Nginx seviyesinde rate limiting yok.**
   Kimliği doğrulanmış bir kullanıcı herhangi bir REST endpoint'ini
   spam'leyebilir. Doğru çözüm yeri nginx (`limit_req`), endpoint'ler değil.

4. **POST endpoint'leri oluşturdukları kaydın `id`'sini döndürmüyor.**
   `POST /accounts` → `{"message": "Account created!", "name": "..."}`. Aynısı
   categories, transactions ve budgets için de geçerli. Bir kayıt oluşturup ona
   referans vermek isteyen client fazladan bir GET/List çağırmak zorunda.
   REST alışkanlığı: `201` ile birlikte oluşan kaynağı (ya da en az `id`'yi) ve
   bir `Location` başlığı döndürmek. Etkisi düşük ama her client'ı gereksiz bir
   tur atmaya zorluyor. (Postman koleksiyonunda `account_id`'yi elle girmek
   zorunda olmanın sebebi bu.)

5. **`POST /transactions` `category_id` sahipliğini doğrulamıyor.**
   `handlers/transaction_handlers.go` içinde "categor" geçen tek satır yok.
   `account_id` için `getAccountForRequest` var, `category_id` için hiçbir şey.
   Oysa `chat/resolve.go`'daki `resolveCategory` bunu titizce yapıyor — yani
   **AI yolu REST yolundan daha sıkı.**

   *Yapabildiği:* kullanıcı başkasının kategori id'siyle kendi hesabına işlem
   yazabilir (FK sayesinde kategori var olmak zorunda, ama kimin olduğu
   kontrol edilmiyor).
   *Yapamadığı:* veri sızıntısı yok — listeleme hesap üzerinden, hesaplar
   kullanıcı üzerinden kapsanıyor.
   *Gerçek etkisi:* (a) veri bütünlüğü — işlem, kullanıcının sahip olmadığı bir
   kategoriye etiketlenir ve bütçe hesabında sessizce sayılmaz; (b) zayıf varlık
   kehaneti — geçersiz id 500, geçerli id 201 döndüğü için hangi id'lerin var
   olduğu öğrenilebilir.
   *Ciddiyet:* düşük-orta. Veri ihlali değil, bütünlük açığı.
   *Doğru çözüm:* `resolveCategory`'nin mantığını ortak bir yere alıp iki
   taraftan da çağırmak — handler'a ayrı bir kopya yazmak üçüncü bir ayrışma
   kaynağı yaratır.

6. **Diğer bilinen eksikler:** `TRUSTED_PROXIES` dokümante değil · LLM token
   bazlı maliyet limiti yok · access token toplu iptali yok · repository
   katmanında birim testi yok · `repositories/integration_test.go` derlenmiyor
   (context parametresi eksik) · TLS yapılandırılmadı

---

## Kendini sına

Cevaba bakmadan önce yüksek sesle anlat. Takıldığın yer bir sonraki oturumun
konusu.

<details>
<summary>Model "kahve 50 lira" için amount 5000 üretti. Doğrulama katmanı bunu neden yakalayamaz?</summary>

Çünkü 5000 **yapısal olarak kusursuz**: pozitif, geçerli kategoriye bağlı,
kullanıcının hesabında. Kod anlamsal doğruluğu bilemez — birinin gerçekten
5000 TL'lik bir kahve makinesi alması mümkün. Bunu yalnızca kullanıcı bilebilir,
bu yüzden taslak ona gösterilir.
</details>

<details>
<summary>Chat kendi yazma yolunu kullansaydı, POST /transactions'a eklenen yeni bir kural ne olurdu?</summary>

İki yere yazılması gerekirdi. Biri unutulursa: kullanıcı hata görmez, 200 döner,
veritabanına kuralsız kayıt yazılır, testler yeşil kalır, derleyici susar. Aylar
sonra bozuk veriden fark edilir.
</details>

<details>
<summary>Neden max=500'ün iki yerde olması kabul edilebilir ama yazma kuralının olması değil?</summary>

Ayrışmanın **etki yarıçapı** farklı. `max=500` ayrışırsa: gürültülü, anlık, veri
sağlam. Yazma kuralı ayrışırsa: sessiz, gecikmeli, veri kalıcı olarak bozuk.
</details>

<details>
<summary>authMem ile chatMem aynı tipte. O zaman authLimiter'ın AllowWithLimit'i neden çağrılamaz?</summary>

`var authLimiter middleware.Limiter = authMem` — değişken dar arayüzle ilan
edildi, metot bilerek atıldı. `LimitByPlan(authLimiter, ...)` derlenmez. Yetenek
var ama pencere kapalı.
</details>

<details>
<summary>AuthMiddleware, LimitByPlan'dan önce olmasaydı ne olurdu?</summary>

`chatPlanFn` içindeki `c.MustGet("user_id")` panikler, `gin.Recovery()` yakalar,
her `/chat` isteği 500 döner. Ayrıca kullanıcı/plan bilinmeden plan bazlı limit
zaten anlamsız olurdu.
</details>

<details>
<summary>Uydurma bir onay token'ıyla /actions/confirm'e istek atılırsa sunucu ne yapar?</summary>

`s.pending.Claim(...)` veritabanına gider, "böyle token yok" cevabı alır, 400
döner. **Reddedilmek bedava değil** — en az bir DB sorgusu harcanır. 1000 uydurma
token = 1000 sorgu.
</details>

<details>
<summary>Model bilinmeyen bir kategori (id 999) önerdi. Sistem ne yapar, neden reddetmez?</summary>

Kategoriyi düşürür, uyarı ekler, `category_id`'yi `NeedsInput`'a koyar ve
`Payload` üretmez — yani kullanıcıya sorar. Reddetmez çünkü iş kurtarılabilir;
uydurmaz çünkü doğru kategoriyi yalnızca kullanıcı bilir.
</details>

---

## Kişisel kör noktalar

Tekrar eden hatalar. Sonraki oturumlarda kasten yoklanacak.

1. **Sonucu düşünüp maliyeti düşünmemek.** "Reddedildi" (evet) / "reddetmek ne
   kadar iş" (atlanıyor). Bir oturumda üç kez çıktı. Güvenlikte asıl olan
   ikincisi — bütün DoS saldırıları "reddedilen isteğin de maliyeti var"
   gerçeğine dayanır.

2. **Mekanizmayı hatırlamak yerine tahmin üretmek.** "Regex'le mi çekiyorduk?"
   Doğru refleks: *"bir bakayım"* + `grep`. Repoda 10 saniyede bulunur.

3. **Rota satırına bakıp middleware zincirini eksik okumak.** Grup middleware'i
   yukarıda tanımlanır, satırda görünmez.

4. **Dosya envanteri çıkarıp sorumluluk cümlesi yazmamak.** "İçinde ne var" ile
   "neyden sorumlu" farklı sorular. İkincisi paketin sınırını çizer, birincisi
   çizmez.

5. **Dosyayı eksik okuyup tamamı sanmak.** `models/budget.go`'nun %70'i
   (10 tipin tamamı) ve `auth/cookie.go`'nun hepsi atlandı — ikisi de kendi
   paketlerinin **en büyük** dosyası. Refleks: paketi okumadan önce
   `wc -l` çek, en büyük dosyayı atlamadığından emin ol.
