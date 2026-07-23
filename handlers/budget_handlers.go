package handlers

import (
	"GoGinMoneyCopilot/chat"
	"GoGinMoneyCopilot/models"
	"GoGinMoneyCopilot/repositories"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// maxPeriodOffset — geçmişe/geleceğe kaç dönem bakılabileceğinin sınırı.
//
// NEDEN SINIR VAR: offset doğrudan AddDate'e çarpan olarak giriyor. Sınırsız
// bırakılsaydı çok büyük bir offset, gün farkını tutan time.Duration'ı taşırır
// (±292 yıl) ve çöp bir indeks üretirdi. 120 dönem, aylık bütçede 10 yıl eder.
const maxPeriodOffset = 120

type BudgetHandler struct {
	budgets      repositories.BudgetRepository
	categories   repositories.CategoryRepository
	accounts     repositories.AccountRepository
	transactions repositories.TransactionRepository
}

func NewBudgetHandler(budgets repositories.BudgetRepository, categories repositories.CategoryRepository, accounts repositories.AccountRepository, transactions repositories.TransactionRepository) *BudgetHandler {
	return &BudgetHandler{budgets: budgets, categories: categories, accounts: accounts, transactions: transactions}
}

// validateBudgetInput — Create ve Update için ORTAK doğrulama. Hata durumunda
// yanıtı KENDİSİ yazar ve ok=false döner. Sıra ucuzdan pahalıya: girdi kendi
// içinde tutarlı olmadan veritabanına gidilmez.
func (h *BudgetHandler) validateBudgetInput(c *gin.Context, userID int, startDate string, lines []models.BudgetCategoryInput) (time.Time, bool) {
	// 1) Aynı kategori iki kez olamaz. Asıl garanti veritabanındaki
	// uniqueIndex(budget_id, category_id); bu kontrol düzgün hata mesajı
	// vermek ve boşuna bir transaction açmamak için.
	seen := map[int]bool{}
	for _, line := range lines {
		if seen[line.CategoryID] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "A category can only appear once in a budget"})
			return time.Time{}, false
		}
		seen[line.CategoryID] = true
	}

	// 2) Tarih. binding'deki datetime=2006-01-02 formatı zaten doğruladığı
	// için buradaki hata dalı pratikte erişilemez; yine de etiket bir gün
	// kaldırılırsa diye duruyor.
	parsed, err := time.Parse(models.DateLayout, startDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid start date"})
		return time.Time{}, false
	}

	// Gelecek tarih reddedilir: bütçe henüz başlamamışken "şu anki dönem"
	// bütçenin ÖNCESİNDEKİ bir aralığı gösterirdi. TAKVİM GÜNÜ karşılaştırılır,
	// an değil; yoksa saati ileri bir istemciden gelen aynı-gün isteği haksız
	// yere reddedilirdi.
	now := time.Now().In(models.AppLocation())
	if models.CivilDate(parsed).After(models.CivilDate(now)) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Start date cannot be in the future"})
		return time.Time{}, false
	}

	// 3) Kategoriler: TEK sorgu ile kullanıcının görebildiklerini çek. Bilerek
	// N kez GetByID değil — hem N sorgu olurdu hem de GetByID'de sahiplik
	// filtresi yok, başkasının kategorisi bütçeye sızardı. GetForUser global
	// kategorileri (user_id IS NULL) de döner.
	visible, err := h.categories.GetForUser(userID)
	if err != nil {
		respondInternalError(c, err)
		return time.Time{}, false
	}
	byID := make(map[int]models.Category, len(visible))
	for i := range visible {
		byID[visible[i].ID] = visible[i]
	}

	for _, line := range lines {
		cat, ok := byID[line.CategoryID]
		if !ok {
			// Başkasının kategorisi de buraya düşer: varlığını sızdırmadan 404.
			c.JSON(http.StatusNotFound, gin.H{"error": "Category not Found"})
			return time.Time{}, false
		}
		// 4) Bütçe harcamayı sınırlar; gelir kategorisinin limiti anlamsız.
		if cat.Type != "expense" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Only expense categories can be budgeted"})
			return time.Time{}, false
		}
	}

	return parsed, true
}

func (h *BudgetHandler) CreateBudget(c *gin.Context) {
	var input models.CreateBudgetInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input format!"})
		return
	}

	userID := c.MustGet("user_id").(int)
	startDate, ok := h.validateBudgetInput(c, userID, input.StartDate, input.Categories)
	if !ok {
		return
	}

	if err := h.budgets.Create(userID, input, startDate); err != nil {
		if errors.Is(err, repositories.ErrBudgetExists) {
			c.JSON(http.StatusConflict, gin.H{"error": "You already have a budget"})
			return
		}
		respondInternalError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "Budget created!"})
}

// GetBudget — bütçenin bir DÖNEMİNİ hesaplar. offset=0 içinde bulunulan dönem,
// -1 bir önceki. Hiçbir dönem verisi saklanmadığı için geçmiş dönem sorgusu ile
// güncel dönem sorgusu tamamen aynı kodu çalıştırır.
func (h *BudgetHandler) GetBudget(c *gin.Context) {
	offset := 0
	if raw := c.Query("offset"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed > maxPeriodOffset || parsed < -maxPeriodOffset {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid offset"})
			return
		}
		offset = parsed
	}

	userID := c.MustGet("user_id").(int)
	now := time.Now().In(models.AppLocation())

	// Görünüm hesabı chat.BuildBudgetView'de — aynı fonksiyonu chat de çağırır,
	// böylece dönem/toplam/aşım mantığı tek yerde yaşar.
	view, err := chat.BuildBudgetView(h.budgets, h.categories, h.accounts, h.transactions, userID, offset, now)
	if err != nil {
		if errors.Is(err, repositories.ErrBudgetNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Budget not Found"})
			return
		}
		respondInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, view)
}

func (h *BudgetHandler) UpdateBudget(c *gin.Context) {
	userID := c.MustGet("user_id").(int)

	// Bütçe var mı kontrolü JSON bağlamadan ÖNCE — mevcut update
	// handler'larının kasıtlı sırası.
	budget, err := h.budgets.GetForUser(userID)
	if err != nil {
		if errors.Is(err, repositories.ErrBudgetNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Budget not Found"})
			return
		}
		respondInternalError(c, err)
		return
	}

	var input models.UpdateBudgetInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input format"})
		return
	}

	startDate, ok := h.validateBudgetInput(c, userID, input.StartDate, input.Categories)
	if !ok {
		return
	}

	if err := h.budgets.Replace(budget.ID, input, startDate); err != nil {
		if errors.Is(err, repositories.ErrBudgetNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Budget not Found!"})
			return
		}
		respondInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Budget updated!"})
}

func (h *BudgetHandler) DeleteBudget(c *gin.Context) {
	userID := c.MustGet("user_id").(int)

	budget, err := h.budgets.GetForUser(userID)
	if err != nil {
		if errors.Is(err, repositories.ErrBudgetNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Budget not Found"})
			return
		}
		respondInternalError(c, err)
		return
	}

	if err := h.budgets.Delete(budget.ID); err != nil {
		if errors.Is(err, repositories.ErrBudgetNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Budget not Found!"})
			return
		}
		respondInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Budget deleted!"})
}
