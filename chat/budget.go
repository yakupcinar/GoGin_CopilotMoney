package chat

import (
	"GoGinMoneyCopilot/models"
	"GoGinMoneyCopilot/repositories"
	"errors"
	"fmt"
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
			"zaten bir bütçeniz var; değiştirmek için bütçe panelini kullanın")
	} else if !errors.Is(err, repositories.ErrBudgetNotFound) {
		// Altyapı hatası: detayı sızdırma.
		return nil, warnings, needsInput, fmt.Errorf("bütçe durumu kontrol edilemedi")
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
		return nil, warnings, needsInput, fmt.Errorf("dönem en fazla 365 gün olabilir")
	}

	// --- her satırı çöz: ref -> id, tip ve limit kontrolü, yinelenme ---
	seen := map[int]bool{}
	lines := make([]models.BudgetCategoryInput, 0, len(p.BudgetCategories))
	for _, bc := range p.BudgetCategories {
		if bc.Amount <= 0 {
			return nil, warnings, needsInput, fmt.Errorf(
				"%q için geçerli bir limit okunamadı", bc.CategoryRef)
		}
		// matchCategory: ismi id'ye çevirir; belirsizse ErrAmbiguousTarget.
		cat, err := matchCategory(categories, bc.CategoryRef)
		if err != nil {
			return nil, warnings, needsInput, fmt.Errorf("%q: %w", bc.CategoryRef, err)
		}
		if cat.Type != "expense" {
			return nil, warnings, needsInput, fmt.Errorf(
				"%q bir gider kategorisi değil, bütçelenemez", cat.Name)
		}
		if seen[cat.ID] {
			return nil, warnings, needsInput, fmt.Errorf(
				"%q kategorisi birden fazla kez verildi", cat.Name)
		}
		seen[cat.ID] = true
		lines = append(lines, models.BudgetCategoryInput{
			CategoryID: cat.ID, LimitAmount: bc.Amount,
		})
	}

	// --- isim: kullanıcı verdiyse onu, yoksa varsayılan ---
	name := p.Name
	if name == "" {
		name = "Bütçe"
	}
	if r := []rune(name); len(r) > 30 {
		name = string(r[:30])
		warnings = append(warnings, "bütçe adı 30 karaktere kırpıldı")
	}

	// --- başlangıç: chat hızlı-giriş içindir, varsayılan bugün ---
	return &models.CreateBudgetInput{
		Name:       name,
		StartDate:  today.Format(models.DateLayout),
		PeriodDays: p.PeriodDays,
		Categories: lines,
	}, warnings, needsInput, nil
}
