Research List!
[YAPILDI] ***Gelen tüm Requestleri logla (print ile yazdır veya farklı dosya oluşturup içine yaz) — RequestLogger middleware
[YAPILDI] **Gelen requestlerin nasıl bir şekilde geldiğini öğren (method/path/data) json ? tokenler burda nerde ?
[YAPILDI] **Middleware document incele araştır (Gin Web Framework)
[YAPILDI] **TCP->GO(net/http)->GIN(gin.Engine.ServeHTTP)->HANDLER(c.ShouldBindJSON(&input)) bu akışı araştır ayrıca HTTP
[YAPILDI] ***ORM araştır — GORM'a geçildi
[YAPILDI] ***Reposority patern araştır — repositories/ paketi, hepsi interface + GORM implementasyonu
**Routelar valueback atıyorlar otomatik c.JSON'la istersen kendin handlelayabilirsin onu bir değişkende saklayıp sonra eklersin response := c.JSON(...) araştır
**db.go log.Fatal'da direkt killemek yerine değeri döndürüp main'e orada checkle kapamak nasıl olurdu
[YAPILDI] **logger.go latency, Status(), requestLog, json.Marshal, ...
[YAPILDI] ***Testcase: Login yapılırken username veya password eşleşmemesi sonucu çıkan hatada yüksek ms farkı var bunu düzelt !!! — dummy hash ile timing side-channel kapatıldı
[YAPILDI] ***JWT token ve diğer token tiplerini derinlemesine araştır! — alg-confusion fix (WithValidMethods), hibrit access+refresh token modeli kuruldu

Todo List!
[YAPILDI] **.env dosyası kurulcak(DB connect bilgileri etc.)
[YAPILDI] **err.Error'lar silincek yerine sunucu hatası print — 24 yerde düzeltildi
**AuthMiddleware sadece c.Next() öncesi çalışıyor sonrası c.Next() öncesi zaman başlatıp logla c.Next() bitişinde printle (example)
**Validation duruma göre güncellenmesi gerekebilir ! (sonra bakılır) — validators/validator.go eklendi, ihtiyaç oldukça genişletilecek
[YAPILDI] ***db.go'da ayrıma git user, transaction migrate etc. (directory/file düzeni getir) — repositories/ katmanına bölündü
**Printleme yapılacak her kullanıcının action'ınını sonuçlarıyla logla ! (farklı bir dosyada loglanabilir)
[YAPILDI] **Route gruplamaya bak dry-code için mantıklı olur ! (auth middleware için)
**Makefile geliştirmeye açık tekrar bakılacak !	
[YAPILDI] **Repository Pattern kullan (sonra yapılacak)
[YAPILDI] **signal.Notify ile SIGINT/SIGTERM'i yakalayıp, http.Server.Shutdown(ctx) ile "mevcut istekleri bitir, yeni istek alma, sonra düzgünce kapan" davranışı eklemek. (CTRL+C ile kapatınca make error alıyor; graceful shutdown'a çevir)
**SSL ekle + Gin releasede çalıştır + Dockerhub + ... (en son sistemi büyütebildiğin kadar büyüt ve test et)
[YAPILDI] ***htp ai entegre kur farklı bir mainde free api key kullan system prompt user prompt denemesi yap, etc.. — Groq entegrasyonu, chat/ + ai/ katmanları, 12 route niyeti destekleniyor (detay: README.AI.md)


[YAPILDI] ***Bütçe (Budget) özelliği (REST) — kategori bazlı harcama limiti, tekrar eden özel uzunlukta dönem (start_date + period_days, 1-365 gün). Kullanıcı başına tek bütçe, toplam limit kategorilerden türetilir. POST/GET/PUT/DELETE /budgets. Dönem sorgu anında hesaplanır, hiçbir dönem verisi saklanmaz (bkz. models/budget.go PeriodAt). ~58 yeni test.

Yeni Todo'lar!
***.env'deki JWT_SECRET geçici değer ("gecici-...") — üretim öncesi mutlaka değiştirilecek (şimdilik bilinçli olarak dokunulmadı)
**middleware/auth.go'daki revoked_tokens sorgusu her istekte çalışıyor — şu an sorun değil, yüksek trafikte cache/index optimizasyonu gerekebilir (bilinçli erken-optimizasyon yapılmadı)
**Rate limiter şu an bellekte (middleware/ratelimit.go) — birden fazla instance ile çalışacaksa Redis'e taşınmalı
**Mevcut testler (105 adet, hepsi geçiyor) CI'a bağlanabilir
[YAPILDI] **Bütçe FK'ları GORM ilişki etiketleriyle modele kondu (models/budget.go): budget_id -> budgets (CASCADE), category_id -> categories (RESTRICT). AutoMigrate bunları otomatik üretiyor; elle SQL migration bağımlılığı yok. Kategori-silme 409'u DB seviyesinde RESTRICT ile de destekleniyor (derinlemesine savunma).
**Bütçe geçmiş dönemleri BUGÜNKÜ limitlerle çizer (limit geçmişi kasıtlı olarak tutulmuyor); yanıttaki historical:true bunu belirtir
**Bütçeye eklendikten sonra income'a çevrilen kategori sonsuza dek 0 harcama raporlar (düşük risk)
**APP_TIMEZONE üretimde ayarlanmalı (bkz. .env.example) — yoksa dönem UTC'ye göre döner


Notes!
**Object-Relational Mapping: Veritabanı tablolarını, senin Go struct'larınla (Object) otomatik eşleştiren (Mapping) bir kütüphane/teknik. Amaç: elle SQL string'i yazıp, elle Scan() ile alan alan struct'a doldurmak yerine, bunu bir kütüphaneye devretmek. GORM go'da kullanılan en yaygın tipi Ex: db.First(&acc, accountID) farklı kullanımları var ama bu ORM oluyor FARKLI veritabanlarında çalışmana yarar!
**Repository Pattern: İş mantığının (handler'ların), veritabanına doğrudan değil, bir soyutlama (interface) üzerinden erişmesini sağlayan bir mimari kalıp. Amaç: "nasıl veri çekiliyor" detayını (SQL, hangi DB motoru) iş mantığından tamamen gizlemek. 
Ex:type AccountRepository interface { Create(name string, userID int) error } type PostgresAccountRepository struct { db *sql.DB }
**AI entegrasyonu detayları için README.AI.md'ye bak (temel ilke, mimari, güvenlik modeli, bulunan gerçek hatalar)
***Refactor ne demek bir func'da kullanılması nasıl olur



Utility Notes!
git push origin main 	
