package models

import (
	"os"
	"time"
)

// Budget — kullanıcının tekrar eden bir dönem boyunca harcama sınırı.
//
// KAPSAM: bütçe KULLANICI seviyesindedir, hesap seviyesinde değil. Bütçe
// paranın AMACI üzerine kurulur (kategori), KAYNAĞI üzerine değil (hesap).
// Hesap başına bölseydik, markete 800'ü nakitten 700'ü karttan ödeyen
// kullanıcı iki ayrı çizelgede 800/1500 ve 700/1500 görürdü — ikisi de
// "limit doldu" demezdi, halbuki dolmuştu. Hesap yalnızca arayüzde bir
// görüntüleme filtresidir, modelin parçası değildir.
//
// TEK BÜTÇE: user_id üzerinde uniqueIndex var. Bunun bir yan faydası, hiçbir
// URL'de bütçe id'si geçmemesi: sahiplik hatası yapılabilecek bir kod yolu
// doğmuyor.
//
// TOPLAM LİMİT SAKLANMAZ: toplam = kategori limitlerinin toplamı. Ayrı bir
// kolon tutsaydık aynı gerçeğin iki kaynağı olurdu ve zamanla ayrışırlardı.
//
// DÖNEM SAKLANMAZ: hiçbir dönem satırı, geçmiş tablosu ya da sıfırlama görevi
// yoktur. Dönem, start_date + period_days ikilisinden SORGU ANINDA türetilir
// (bkz. PeriodAt). Bu sayede kullanıcı start_date'i değiştirdiğinde geçmiş
// dahil her şey kendiliğinden yeniden dilimlenir — ayrı bir "yeniden hesapla"
// adımı yok.
type Budget struct {
	ID         int       `json:"id" gorm:"primaryKey"`
	UserID     int       `json:"user_id" gorm:"not null;uniqueIndex"`
	Name       string    `json:"name" gorm:"size:30;not null"`
	StartDate  time.Time `json:"start_date" gorm:"type:date;not null"`
	PeriodDays int       `json:"period_days" gorm:"not null;check:period_days BETWEEN 1 AND 365"`
	CreatedAt  time.Time `json:"created_at"`

	// Categories — SADECE AutoMigrate'in budget_id foreign key'ini + ON DELETE
	// CASCADE'i üretmesi için var. json:"-" ile API yanıtına sızmaz; kod bu
	// alanı hiçbir zaman doldurmaz (satırlar repo'da ayrı yazılır), bu yüzden
	// GORM'un ilişki-otomatik-kaydetme davranışı bu uçtan tetiklenmez.
	Categories []BudgetCategory `json:"-" gorm:"foreignKey:BudgetID;constraint:OnDelete:CASCADE"`
}

// BudgetCategory — bir bütçenin tek kategori satırı.
//
// Alan adı LimitAmount, Limit değil: "limit" Postgres'te ayrılmış kelimedir ve
// tırnaklanmış bir kolon adı psql'de kullanmayı zorlaştırır.
//
// uniqueIndex(budget_id, category_id): aynı kategori bir bütçeye iki kez
// eklenemez. Bu kontrolü handler'da da yapıyoruz (daha iyi hata mesajı için)
// ama GARANTİ burada: iki eşzamanlı istek Go kontrolünü birlikte geçebilir,
// veritabanı indeksini geçemez.
type BudgetCategory struct {
	ID          int     `json:"id" gorm:"primaryKey"`
	BudgetID    int     `json:"budget_id" gorm:"not null;uniqueIndex:idx_budget_category,priority:1"`
	CategoryID  int     `json:"category_id" gorm:"not null;uniqueIndex:idx_budget_category,priority:2"`
	LimitAmount float64 `json:"limit_amount" gorm:"type:numeric(12,2);not null"`

	// Category — category_id foreign key'ini üretir. ON DELETE RESTRICT:
	// bir bütçede kullanılan kategori DB seviyesinde de silinemez (handler'daki
	// Go 409 kontrolünün üstüne derinlemesine savunma; yarış durumunu kapatır).
	//
	// DİKKAT: bu ilişki alanı yüzünden satır yazarken tx.Omit("Category")
	// kullanılmalı — yoksa GORM sıfır-değerli Category{}'yi yeni bir kayıt
	// sanıp boş bir kategori INSERT etmeye çalışır (bkz. budget_repository.go).
	Category Category `json:"-" gorm:"constraint:OnDelete:RESTRICT"`
}

// BudgetCategoryInput — bütçe isteğindeki tek kategori satırı.
//
// max=99999999: numeric(12,2) 10^10 üstünde taşar ve Postgres 22003 verir, bu
// da 500'e dönüşürdü. Doğrulama katmanında kesmek daha doğru.
type BudgetCategoryInput struct {
	CategoryID  int     `json:"category_id" binding:"required,gt=0"`
	LimitAmount float64 `json:"limit_amount" binding:"required,gt=0,max=99999999"`
}

// CreateBudgetInput — bütçe başlığı ve kategori satırları TEK istekte gelir.
//
// start_date NEDEN string: time.Time olsaydı "2026-06-30T21:00:00Z" ile
// "2026-07-01T00:00:00+03:00" aynı anı gösterip FARKLI takvim günü olurdu;
// istemcinin saat dilimine göre dönem bir gün kayardı. Düz "2006-01-02"
// formatında belirsizlik yok.
//
// dive NEDEN ŞART: validator/v10 iç içe struct ALANLARINA kendiliğinden iner,
// ama slice ELEMANLARINA inmez. dive olmadan 3. elemandaki category_id: 0
// doğrulamayı geçip veritabanına ulaşır.
type CreateBudgetInput struct {
	Name       string                `json:"name" binding:"required,max=30"`
	StartDate  string                `json:"start_date" binding:"required,datetime=2006-01-02"`
	PeriodDays int                   `json:"period_days" binding:"required,min=1,max=365"`
	Categories []BudgetCategoryInput `json:"categories" binding:"required,min=1,max=50,dive"`
}

// UpdateBudgetInput — TAM DEĞİŞTİRME: kategori satırları dahil her şey yeniden
// gönderilir. UpdateTransactionInput ile aynı semantik (bu projede kısmi
// güncelleme/PATCH deyimi yok). Faydası: Replace tek transaction'da sil+ekle
// olur ve /budgets/categories/:id gibi bir rota yüzeyi hiç doğmaz.
type UpdateBudgetInput struct {
	Name       string                `json:"name" binding:"required,max=30"`
	StartDate  string                `json:"start_date" binding:"required,datetime=2006-01-02"`
	PeriodDays int                   `json:"period_days" binding:"required,min=1,max=365"`
	Categories []BudgetCategoryInput `json:"categories" binding:"required,min=1,max=50,dive"`
}

// --- yanıt tipleri ---

// BudgetView — GET /budgets yanıtı. Hiçbir alanı veritabanında saklanmaz;
// hepsi istek anında hesaplanır.
type BudgetView struct {
	Budget     BudgetSummaryView    `json:"budget"`
	Period     PeriodView           `json:"period"`
	TotalLimit float64              `json:"total_limit"`
	TotalSpent float64              `json:"total_spent"`
	TotalLeft  float64              `json:"total_remaining"`
	Categories []BudgetCategoryView `json:"categories"`
}

// BudgetSummaryView — tarihler RFC3339 değil düz "2006-01-02" string'i.
// İstemcinin new Date("...T00:00:00Z") ile bir gün kaydırmasını engeller:
// sunucuda kaçındığımız off-by-one'ı istemciye devretmenin anlamı yok.
type BudgetSummaryView struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	StartDate  string `json:"start_date"`
	PeriodDays int    `json:"period_days"`
}

// PeriodView — Historical, offset != 0 olduğunda true olur. Geçmiş dönemler
// BUGÜNKÜ limitlerle çizilir (limit geçmişi kasıtlı olarak tutulmuyor); bu
// bayrak arayüzün bunu dürüstçe söyleyebilmesi için var.
type PeriodView struct {
	Index         int    `json:"index"`
	Offset        int    `json:"offset"`
	StartDate     string `json:"start_date"`
	EndDate       string `json:"end_date"`
	DaysTotal     int    `json:"days_total"`
	DaysElapsed   int    `json:"days_elapsed"`
	DaysRemaining int    `json:"days_remaining"`
	Historical    bool   `json:"historical"`
}

// BudgetCategoryView — Remaining negatif olabilir ve BİLEREK clamp'lenmez:
// negatif değer, arayüzdeki halkanın kırmızıya dönme sinyalinin ta kendisi.
// Yüzde alanı yok; istemci böler (yuvarlama anlaşmazlığı ve sunucuda sıfıra
// bölme koruması gerekmesin diye).
type BudgetCategoryView struct {
	CategoryID   int     `json:"category_id"`
	CategoryName string  `json:"category_name"`
	LimitAmount  float64 `json:"limit_amount"`
	Spent        float64 `json:"spent"`
	Remaining    float64 `json:"remaining"`
	OverLimit    bool    `json:"over_limit"`
}

// --- dönem matematiği ---

// DateLayout — API'de ve dönem hesabında kullanılan tek tarih formatı.
const DateLayout = "2006-01-02"

// MaxPeriodOffset — geçmişe/geleceğe kaç dönem bakılabileceğinin sınırı.
// Hem HTTP handler'ı (?offset=) hem chat (period_offset) bunu kullanır.
//
// NEDEN SINIR VAR: offset doğrudan AddDate'e çarpan girer. Sınırsız bırakılsa
// çok büyük bir offset, gün farkını tutan time.Duration'ı taşırır (±292 yıl)
// ve çöp bir indeks üretir. 120 dönem, aylık bütçede 10 yıl eder.
const MaxPeriodOffset = 120

// AppLocation — dönem hesabının hangi takvimde yapılacağı.
//
// Konteynerler varsayılan olarak TZ=UTC ile çalışır. Bu ayar olmasaydı
// Türkiye'deki bir kullanıcının dönemi gece 21:00'de dönerdi ve bu hiçbir
// yerde hata vermezdi — sadece sessizce yanlış gün gösterirdi.
func AppLocation() *time.Location {
	loc, err := time.LoadLocation(os.Getenv("APP_TIMEZONE"))
	if err != nil {
		return time.UTC
	}
	return loc
}

// CivilDate — bir zamanı takvim gününe indirger: saat, dakika ve saat dilimi
// atılır, sonuç UTC gün başıdır.
//
// NEDEN UTC'ye SABİTLİYORUZ: iki takvim gününü çıkardığımızda farkın tam 24
// saatin katı olmasını istiyoruz. Yerel saat diliminde bir "gün" DST geçişinde
// 23 veya 25 saat sürebilir; UTC'de asla.
//
// NEDEN t.Date() (t.UTC().Date() DEĞİL): kullanıcının kastettiği takvim günü,
// t'nin kendi dilimindeki gündür. UTC'ye çevirip okusaydık gece yarısından
// sonraki ilk saatlerde bir önceki günü görürdük.
func CivilDate(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// daysBetween — iki takvim günü arasındaki tam gün sayısı. Her iki uç da UTC
// gün başına indirgendiği için fark her zaman 24 saatin tam katıdır; kalan
// (ve dolayısıyla yuvarlama hatası) oluşamaz.
func daysBetween(from, to time.Time) int {
	return int(CivilDate(to).Sub(CivilDate(from)) / (24 * time.Hour))
}

// floorDiv — AŞAĞI yuvarlayan tamsayı bölmesi.
//
// Go'nun / operatörü sıfıra doğru kırpar: -5 / 30 == 0 sonucu "bugün 0.
// dönemdeyiz" der. Halbuki bugün 0. dönemin ÖNCESİNDEdir, yani -1. döneminde.
// Bu tam olarak sessizce yanlış cevap veren türden bir hatadır: patlamaz,
// sadece yanlış aralığı toplar.
func floorDiv(a, b int) int {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

// Period — bir dönemin YARI AÇIK [Start, End) aralığı.
type Period struct {
	Index int
	Start time.Time
	End   time.Time
}

// PeriodAt — offset numaralı dönemin aralığı.
// offset=0 bugünün dönemi, -1 bir öncekisi, +1 bir sonrakisi.
//
//	N. dönem   = [start_date + N*period_days, start_date + (N+1)*period_days)
//	Bugünün N'i = floor((bugün - start_date) / period_days)
//
// YARI AÇIK OLMASI ŞART: kapalı aralık kullansaydık dönem sonu ile bir sonraki
// dönemin başlangıcı AYNI GÜN olurdu ve o günün harcamaları iki dönemde
// birden sayılırdı.
func (b *Budget) PeriodAt(today time.Time, offset int) Period {
	// period_days hem binding etiketiyle hem DB check kısıtıyla 1..365
	// arasında tutuluyor. Yine de sıfır değerli bir Budget struct'ı (test,
	// gelecekteki bir çağrı yolu) burada sıfıra bölmeye yol açmasın.
	days := b.PeriodDays
	if days < 1 {
		days = 1
	}

	n := floorDiv(daysBetween(b.StartDate, today), days) + offset
	start := CivilDate(b.StartDate).AddDate(0, 0, n*days)
	return Period{Index: n, Start: start, End: start.AddDate(0, 0, days)}
}

// DaysRemaining — dönemin bitişine kalan gün. Geçmiş dönemler için 0.
func (p Period) DaysRemaining(today time.Time) int {
	if d := daysBetween(today, p.End); d > 0 {
		return d
	}
	return 0
}

// DaysElapsed — dönemin başından bugüne geçen gün, dönem uzunluğuna
// clamp'lenir (geçmiş dönemde tamamı, gelecek dönemde 0).
func (p Period) DaysElapsed(today time.Time) int {
	total := daysBetween(p.Start, p.End)
	d := daysBetween(p.Start, today)
	switch {
	case d < 0:
		return 0
	case d > total:
		return total
	default:
		return d
	}
}
