package chat

import (
	"GoGinMoneyCopilot/models"
	"GoGinMoneyCopilot/repositories"
	"errors"
	"fmt"
	"strings"
	"time"
)

// buildBudget — budget_set niyetinin doğrulaması.
//
// buildTransaction ile aynı felsefe: model bir ÖNERİ verir, burası onu
// doğrulanmış bir taslağa (CreateBudgetInput) çevirir ya da reddeder. Chat
// hiçbir şey YAZMAZ; taslağı frontend POST /budgets ile gönderir, gerçek
// doğrulama REST kapısında da tekrar çalışır.
//
// Kullanıcı başına tek bütçe: bu dilim yalnızca OLUŞTURMAYI kapsar. Zaten
// bütçe varsa oluşturma REST'te 409 olurdu; burada erkenden anlaşılır bir
// mesajla durduruyoruz.
func (s *ActionService) buildBudget(a *models.ParsedAction, req ChatRequest,
	categories []models.Category, today time.Time) (*models.CreateBudgetInput, []string, []string, error) {

	p := a.Params
	var warnings, needsInput []string

	// --- zaten bütçe var mı? (create -> varsa çakışma) ---
	if _, err := s.budgets.GetForUser(req.UserID); err == nil {
		return nil, warnings, needsInput, fmt.Errorf(
			"you already have a budget; use the budget panel to modify it")
	} else if !errors.Is(err, repositories.ErrBudgetNotFound) {
		// Altyapı hatası: detayı sızdırma.
		return nil, warnings, needsInput, fmt.Errorf("failed to check budget status")
	}

	// --- eksik alanlar: değer UYDURMA, kullanıcıya sor ---
	if p.PeriodDays < 1 {
		needsInput = append(needsInput, "period_days")
	}
	if len(p.BudgetCategories) == 0 {
		needsInput = append(needsInput, "budget_categories")
	}
	if len(needsInput) > 0 {
		return nil, warnings, needsInput, nil
	}

	// --- dönem üst sınırı (REST'teki 1..365 ile aynı) ---
	if p.PeriodDays > 365 {
		return nil, warnings, needsInput, fmt.Errorf("the period can be at most 365 days")
	}

	// --- her satırı çöz: ref -> id, tip ve limit kontrolü, yinelenme ---
	seen := map[int]bool{}
	lines := make([]models.BudgetCategoryInput, 0, len(p.BudgetCategories))
	for _, bc := range p.BudgetCategories {
		if bc.Amount <= 0 {
			return nil, warnings, needsInput, fmt.Errorf(
				"could not read a valid limit for %q", bc.CategoryRef)
		}
		// matchCategory: ismi id'ye çevirir; belirsizse ErrAmbiguousTarget.
		cat, err := matchCategory(categories, bc.CategoryRef)
		if err != nil {
			return nil, warnings, needsInput, fmt.Errorf("%q: %w", bc.CategoryRef, err)
		}
		if cat.Type != "expense" {
			return nil, warnings, needsInput, fmt.Errorf(
				"%q is not an expense category and cannot be budgeted", cat.Name)
		}
		if seen[cat.ID] {
			return nil, warnings, needsInput, fmt.Errorf(
				"category %q was provided more than once", cat.Name)
		}
		seen[cat.ID] = true
		lines = append(lines, models.BudgetCategoryInput{
			CategoryID: cat.ID, LimitAmount: bc.Amount,
		})
	}

	// --- isim: kullanıcı verdiyse onu, yoksa varsayılan ---
	name := p.Name
	if name == "" {
		name = "Budget"
	}
	if r := []rune(name); len(r) > 30 {
		name = string(r[:30])
		warnings = append(warnings, "budget name truncated to 30 characters")
	}

	// --- başlangıç: chat hızlı-giriş içindir, varsayılan bugün ---
	return &models.CreateBudgetInput{
		Name:       name,
		StartDate:  today.Format(models.DateLayout),
		PeriodDays: p.PeriodDays,
		Categories: lines,
	}, warnings, needsInput, nil
}

// buildBudgetUpdate — budget_update için MEVCUT bütçeyi çekip üstüne kullanıcının
// değişikliklerini bindirir (oku-değiştir-yaz).
//
// NEDEN MERGE: PUT tam-değiştirme olduğu için tüm satırları yeniden üretmek
// ZORUNDAYIZ. Ama kullanıcı "market'i 2000 yap" dediğinde diğer satırları
// KORUMALIYIZ — o yüzden mevcut limitlerin üstüne yalnızca belirtilenleri
// bindiriyoruz. confirmUpdateTransaction'daki mantığın kategori listesiyle hali.
//
// NEDEN ORTAK (prepare + confirm): confirm bu fonksiyonu GÜNCEL duruma karşı
// yeniden çalıştırır — token beklerken kategori silinmiş/değişmişse yakalar
// (TOCTOU-güvenli). Prepare yalnızca doğrular + özet üretir.
//
// KAPSAM: kategori limiti ekle/değiştir + dönem/isim. Kategori ÇIKARMA ve
// başlangıç tarihi bu dilimde yok (panelden; start_date dönemleri yeniden
// dilimler, sessizce yapılmamalı).
func (s *ActionService) buildBudgetUpdate(userID int, p models.ActionParams) (
	*models.UpdateBudgetInput, *models.Budget, string, error) {

	budget, err := s.budgets.GetForUser(userID)
	if err != nil {
		return nil, nil, "", err // ErrBudgetNotFound dahil
	}
	lines, err := s.budgets.ListCategories(budget.ID)
	if err != nil {
		return nil, nil, "", err
	}
	categories, err := s.categories.GetForUser(userID)
	if err != nil {
		return nil, nil, "", err
	}

	// Mevcut limitler (id -> limit) + satır sırası; değişiklikleri üstüne bindir.
	limits := make(map[int]float64, len(lines))
	order := make([]int, 0, len(lines))
	for _, ln := range lines {
		limits[ln.CategoryID] = ln.LimitAmount
		order = append(order, ln.CategoryID)
	}

	var changes []string

	// --- header: isim / dönem ---
	name := budget.Name
	if p.Name != "" {
		name = p.Name
		if r := []rune(name); len(r) > 30 {
			name = string(r[:30])
		}
		changes = append(changes, fmt.Sprintf("name → %q", name))
	}
	periodDays := budget.PeriodDays
	if p.PeriodDays >= 1 {
		if p.PeriodDays > 365 {
			return nil, nil, "", validationErrorf("the period can be at most 365 days")
		}
		periodDays = p.PeriodDays
		changes = append(changes, fmt.Sprintf("period → %d days", periodDays))
	}

	// --- kategori limiti ekle/değiştir ---
	for _, bc := range p.BudgetCategories {
		if bc.Amount <= 0 {
			return nil, nil, "", validationErrorf("could not read a valid limit for %q", bc.CategoryRef)
		}
		cat, err := matchCategory(categories, bc.CategoryRef)
		if err != nil {
			return nil, nil, "", err
		}
		if cat.Type != "expense" {
			return nil, nil, "", validationErrorf("%q is not an expense category and cannot be budgeted", cat.Name)
		}
		if _, exists := limits[cat.ID]; exists {
			changes = append(changes, fmt.Sprintf("%s → %.2f", cat.Name, bc.Amount))
		} else {
			order = append(order, cat.ID)
			changes = append(changes, fmt.Sprintf("+%s: %.2f", cat.Name, bc.Amount))
		}
		limits[cat.ID] = bc.Amount
	}

	if len(changes) == 0 {
		return nil, nil, "", validationErrorf("nothing to change was specified")
	}

	// Tam satır listesini yeniden kur (sıra korunur).
	catLines := make([]models.BudgetCategoryInput, 0, len(order))
	for _, id := range order {
		catLines = append(catLines, models.BudgetCategoryInput{CategoryID: id, LimitAmount: limits[id]})
	}

	input := &models.UpdateBudgetInput{
		Name:       name,
		StartDate:  budget.StartDate.Format(models.DateLayout), // başlangıç KORUNUR
		PeriodDays: periodDays,
		Categories: catLines,
	}
	return input, budget, strings.Join(changes, ", "), nil
}
