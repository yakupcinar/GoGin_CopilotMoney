package chat

import (
	"GoGinMoneyCopilot/models"
	"GoGinMoneyCopilot/repositories"
	"math"
	"time"
)

// round2 — para değerlerini iki ondalığa yuvarlar. numeric(12,2) toplamları
// float64'te 8420.499999999999 gibi çıkabiliyor; ekrana o gitmesin.
func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

// BuildBudgetView — bir bütçenin offset numaralı DÖNEMİNİ hesaplayıp görünüm
// nesnesini üretir.
//
// NEDEN BURADA (chat paketinde): HEM HTTP handler'ı HEM chat aynı fonksiyonu
// çağırır. Dönem/toplam/aşım hesabı tek yerde yaşasın, iki uçta ayrışmasın —
// projenin "karar mantığı chat'te, arayüz onu çağırır" ilkesi.
//
// SAHİPLİK: GetForUser ve ListForUser sorguyu user_id ile filtreler; bu
// fonksiyon başka kullanıcının verisine erişemez, ayrı bir kontrol gerekmez.
//
// HATA: bütçe yoksa repositories.ErrBudgetNotFound döner (çağıran 404'e
// çevirir). Diğer hatalar sarmalanmış altyapı hatasıdır (çağıran 500'e çevirir).
func BuildBudgetView(
	budgets repositories.BudgetRepository,
	categories repositories.CategoryRepository,
	accounts repositories.AccountRepository,
	txs repositories.TransactionRepository,
	userID, offset int, now time.Time,
) (models.BudgetView, error) {

	budget, err := budgets.GetForUser(userID)
	if err != nil {
		return models.BudgetView{}, err
	}

	lines, err := budgets.ListCategories(budget.ID)
	if err != nil {
		return models.BudgetView{}, err
	}

	// Harcama kullanıcının TÜM hesaplarından toplanır: bütçe kullanıcı
	// seviyesindedir, hesap yalnızca arayüz filtresidir.
	accs, err := accounts.ListForUser(userID)
	if err != nil {
		return models.BudgetView{}, err
	}
	accountIDs := make([]int, 0, len(accs))
	for i := range accs {
		accountIDs = append(accountIDs, accs[i].ID)
	}

	period := budget.PeriodAt(now, offset)

	spent, err := txs.SumExpenseByCategory(accountIDs, period.Start, period.End)
	if err != nil {
		return models.BudgetView{}, err
	}

	// Kategori adları için tek ek sorgu — satır başına sorgu (N+1) değil.
	cats, err := categories.GetForUser(userID)
	if err != nil {
		return models.BudgetView{}, err
	}
	names := make(map[int]string, len(cats))
	for i := range cats {
		names[cats[i].ID] = cats[i].Name
	}

	view := models.BudgetView{
		Budget: models.BudgetSummaryView{
			ID:         budget.ID,
			Name:       budget.Name,
			StartDate:  budget.StartDate.Format(models.DateLayout),
			PeriodDays: budget.PeriodDays,
		},
		Period: models.PeriodView{
			Index:         period.Index,
			Offset:        offset,
			StartDate:     period.Start.Format(models.DateLayout),
			EndDate:       period.End.Format(models.DateLayout),
			DaysTotal:     budget.PeriodDays,
			DaysElapsed:   period.DaysElapsed(now),
			DaysRemaining: period.DaysRemaining(now),
			// Geçmiş/gelecek dönemler BUGÜNKÜ limitlerle çizilir (limit geçmişi
			// tutulmuyor). Arayüz bunu söyleyebilsin diye bayrak.
			Historical: offset != 0,
		},
		Categories: make([]models.BudgetCategoryView, 0, len(lines)),
	}

	for _, line := range lines {
		s := round2(spent[line.CategoryID])
		view.Categories = append(view.Categories, models.BudgetCategoryView{
			CategoryID:   line.CategoryID,
			CategoryName: names[line.CategoryID],
			LimitAmount:  round2(line.LimitAmount),
			Spent:        s,
			// Negatif kalabilir ve BİLEREK clamp'lenmez: limit aşımının sinyali.
			Remaining: round2(line.LimitAmount - s),
			OverLimit: s > line.LimitAmount,
		})
		view.TotalLimit += line.LimitAmount
		view.TotalSpent += s
	}
	// Toplam limit saklanmaz, satırlardan toplanır.
	view.TotalLeft = round2(view.TotalLimit - view.TotalSpent)
	view.TotalLimit = round2(view.TotalLimit)
	view.TotalSpent = round2(view.TotalSpent)

	return view, nil
}
