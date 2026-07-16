Research List!
***Gelen tüm Requestleri logla (print ile yazdır veya farklı dosya oluşturup içine yaz)
**Gelen requestlerin nasıl bir şekilde geldiğini öğren (method/path/data) json ? tokenler burda nerde ?
**Middleware document incele araştır (Gin Web Framework)
**TCP->GO(net/http)->GIN(gin.Engine.ServeHTTP)->HANDLER(c.ShouldBindJSON(&input)) bu akışı araştır ayrıca HTTP
***ORM araştır
***Reposority patern araştır
**Routelar valueback atıyorlar otomatik c.JSON'la istersen kendin handlelayabilirsin onu bir değişkende saklayıp sonra eklersin response := c.JSON(...) araştır
**db.go log.Fatal'da direkt killemek yerine değeri döndürüp main'e orada checkle kapamak nasıl olurdu
**logger.go latency, Status(), requestLog, json.Marshal, ...
***Testcase: Login yapılırken username veya password eşleşmemesi sonucu çıkan hatada yüksek ms farkı var bunu düzelt !!!
***JWT token ve diğer token tiplerini derinlemesine araştır!

Todo List!
**.env dosyası kurulcak(DB connect bilgileri etc.)
**err.Error'lar silincek yerine sunucu hatası print
**AuthMiddleware sadece c.Next() öncesi çalışıyor sonrası c.Next() öncesi zaman başlatıp logla c.Next() bitişinde printle (example)
**Validation duruma göre güncellenmesi gerekebilir ! (sonra bakılır)
***db.go'da ayrıma git user, transaction migrate etc. (directory/file düzeni getir)
***Printleme yapılacak her kullanıcının action'ınını sonuçlarıyla logla ! (farklı bir dosyada loglanabilir)
**Route gruplamaya bak dry-code için mantıklı olur ! (auth middleware için)
**Makefile geliştirmeye açık tekrar bakılacak !	
**Repository Pattern kullan (sonra yapılacak)
**signal.Notify ile SIGINT/SIGTERM'i yakalayıp, http.Server.Shutdown(ctx) ile "mevcut istekleri bitir, yeni istek alma, sonra düzgünce kapan" davranışı eklemek. (CTRL+C ile kapatınca make error alıyor; graceful shutdown'a çevir)
**SSL ekle + Gin releasede çalıştır + Dockerhub + ... (en son sistemi büyütebildiğin kadar büyüt ve test et)




Notes !
**Object-Relational Mapping: Veritabanı tablolarını, senin Go struct'larınla (Object) otomatik eşleştiren (Mapping) bir kütüphane/teknik. Amaç: elle SQL string'i yazıp, elle Scan() ile alan alan struct'a doldurmak yerine, bunu bir kütüphaneye devretmek. GORM go'da kullanılan en yaygın tipi Ex: db.First(&acc, accountID) farklı kullanımları var ama bu ORM oluyor FARKLI veritabanlarında çalışmana yarar!
**Repository Pattern: İş mantığının (handler'ların), veritabanına doğrudan değil, bir soyutlama (interface) üzerinden erişmesini sağlayan bir mimari kalıp. Amaç: "nasıl veri çekiliyor" detayını (SQL, hangi DB motoru) iş mantığından tamamen gizlemek. 
Ex:type AccountRepository interface { Create(name string, userID int) error } type PostgresAccountRepository struct { db *sql.DB }