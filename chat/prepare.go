package chat

import (
	"GoGinMoneyCopilot/models"
	"GoGinMoneyCopilot/repositories"
	"errors"
	"fmt"
)

// Yıkıcı işlemlerin HAZIRLIK adımı: hedefi çöz, ön kontrolleri yap,
// özet üret, onay kodu ver.
//
// ÖN KONTROL NEDEN ÖNEMLİ:
// Gerçekleşemeyeceğini baştan bildiğimiz bir işlem için kullanıcıya
// "Emin misiniz?" diye sormak yanlıştır. Kullanıcı "Evet"e basar, sonra
// hata alır. O yüzden kategori kullanımdaysa onay kodu HİÇ üretilmez.

func (s *ActionService) prepareCategoryAction(res *Result, a *models.ParsedAction,
	req ChatRequest, categories []models.Category) {

	cat, err := s.resolveCategory(a.Params, req)
	if err != nil {
		res.Error = err.Error()
		return
	}

	used, err := s.txs.CountByCategory(cat.ID)
	if err != nil {
		res.Error = "failed to check category usage"
		return
	}

	if a.Intent == models.IntentDeleteCategory && used > 0 {
		res.Error = fmt.Sprintf(
			"category %q is used by %d transactions and cannot be deleted. "+
				"Delete those transactions first or move them to another category.",
			cat.Name, used)
		return
	}

	summary := fmt.Sprintf("%s: category %q (id=%d, %s)",
		verbOf(a.Intent), cat.Name, cat.ID, cat.Type)
	if used > 0 {
		summary += fmt.Sprintf(" — used by %d transactions", used)
	}
	if a.Intent == models.IntentUpdateCategory && a.Params.Name != "" {
		summary += fmt.Sprintf(" → new name: %q", a.Params.Name)
	}

	s.attachConfirmation(res, req.UserID, a.Intent, cat.ID, summary, a.Params)
}

func (s *ActionService) prepareAccountAction(res *Result, a *models.ParsedAction, req ChatRequest) {
	acc, err := s.resolveAccount(a.Params, req)
	if err != nil {
		res.Error = err.Error()
		return
	}

	txs, err := s.txs.ListByAccount(acc.ID)
	if err != nil {
		res.Error = "failed to check account transactions"
		return
	}

	if a.Intent == models.IntentDeleteAccount && len(txs) > 0 {
		res.Error = fmt.Sprintf(
			"account %q has %d transactions and cannot be deleted. Delete the transactions first.",
			acc.Name, len(txs))
		return
	}

	summary := fmt.Sprintf("%s: account %q (id=%d)", verbOf(a.Intent), acc.Name, acc.ID)
	if len(txs) > 0 {
		summary += fmt.Sprintf(" — contains %d transactions", len(txs))
	}
	if a.Intent == models.IntentUpdateAccount && a.Params.Name != "" {
		summary += fmt.Sprintf(" → new name: %q", a.Params.Name)
	}

	s.attachConfirmation(res, req.UserID, a.Intent, acc.ID, summary, a.Params)
}

func (s *ActionService) prepareTransactionAction(res *Result, a *models.ParsedAction, req ChatRequest) {
	tx, err := s.resolveTransaction(a.Params, req)
	if err != nil {
		res.Error = err.Error()
		return
	}

	summary := fmt.Sprintf("%s: %.2f TL %q (%s)",
		verbOf(a.Intent), tx.Amount, tx.Description,
		tx.TransactionDate.Format("2006-01-02"))

	s.attachConfirmation(res, req.UserID, a.Intent, tx.ID, summary, a.Params)
}

// prepareBudgetAction — bütçe silme hazırlığı.
//
// Bütçe kullanıcı başına TEKİL: hedefi çözmek için isim/ref gerekmez,
// GetForUser yeterli. Silmeyi engelleyen bir "kullanımda" durumu yok
// (budget_categories bütçeyle birlikte gider), o yüzden ön kontrol sadece
// "bütçe var mı".
func (s *ActionService) prepareBudgetAction(res *Result, a *models.ParsedAction, req ChatRequest) {
	budget, err := s.budgets.GetForUser(req.UserID)
	if err != nil {
		if errors.Is(err, repositories.ErrBudgetNotFound) {
			res.Error = "you don't have a budget to delete"
			return
		}
		res.Error = "failed to check the budget"
		return
	}

	lines, err := s.budgets.ListCategories(budget.ID)
	if err != nil {
		res.Error = "failed to check the budget"
		return
	}

	summary := fmt.Sprintf("%s: budget %q (%d categories, %d-day period)",
		verbOf(a.Intent), budget.Name, len(lines), budget.PeriodDays)

	s.attachConfirmation(res, req.UserID, a.Intent, budget.ID, summary, a.Params)
}

// prepareBudgetUpdateAction — bütçe değiştirme hazırlığı.
//
// Asıl merge/doğrulama buildBudgetUpdate'te; burada onu bir kez çalıştırıp
// (fail-fast + özet) token üretiyoruz. Üretilen taslak ATILIR — confirm onu
// güncel duruma karşı yeniden kurar (TOCTOU).
func (s *ActionService) prepareBudgetUpdateAction(res *Result, a *models.ParsedAction, req ChatRequest) {
	_, budget, summary, err := s.buildBudgetUpdate(req.UserID, a.Params)
	if err != nil {
		if errors.Is(err, repositories.ErrBudgetNotFound) {
			res.Error = "you don't have a budget to modify"
			return
		}
		res.Error = err.Error()
		return
	}
	full := fmt.Sprintf("%s: budget %q — %s", verbOf(a.Intent), budget.Name, summary)
	s.attachConfirmation(res, req.UserID, a.Intent, budget.ID, full, a.Params)
}

// verbOf — özet metninin başına gelen fiil. Kullanıcı ne olacağını
// tek bakışta görsün diye büyük harf.
func verbOf(i models.Intent) string {
	switch i {
	case models.IntentDeleteAccount, models.IntentDeleteCategory, models.IntentDeleteTransaction,
		models.IntentBudgetDelete:
		return "WILL BE DELETED"
	default:
		return "WILL BE MODIFIED"
	}
}
