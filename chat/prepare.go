package chat

import (
	"GoGinMoneyCopilot/models"
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
		res.Error = "kategori kullanımı kontrol edilemedi"
		return
	}

	if a.Intent == models.IntentDeleteCategory && used > 0 {
		res.Error = fmt.Sprintf(
			"%q kategorisi %d işlemde kullanılıyor, silinemez. "+
				"Önce o işlemleri silin ya da başka bir kategoriye taşıyın.",
			cat.Name, used)
		return
	}

	summary := fmt.Sprintf("%s: %q kategorisi (id=%d, %s)",
		verbOf(a.Intent), cat.Name, cat.ID, cat.Type)
	if used > 0 {
		summary += fmt.Sprintf(" — %d işlemde kullanılıyor", used)
	}
	if a.Intent == models.IntentUpdateCategory && a.Params.Name != "" {
		summary += fmt.Sprintf(" → yeni ad: %q", a.Params.Name)
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
		res.Error = "hesap işlemleri kontrol edilemedi"
		return
	}

	if a.Intent == models.IntentDeleteAccount && len(txs) > 0 {
		res.Error = fmt.Sprintf(
			"%q hesabında %d işlem var, silinemez. Önce işlemleri silin.",
			acc.Name, len(txs))
		return
	}

	summary := fmt.Sprintf("%s: %q hesabı (id=%d)", verbOf(a.Intent), acc.Name, acc.ID)
	if len(txs) > 0 {
		summary += fmt.Sprintf(" — %d işlem içeriyor", len(txs))
	}
	if a.Intent == models.IntentUpdateAccount && a.Params.Name != "" {
		summary += fmt.Sprintf(" → yeni ad: %q", a.Params.Name)
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

// verbOf — özet metninin başına gelen fiil. Kullanıcı ne olacağını
// tek bakışta görsün diye büyük harf.
func verbOf(i models.Intent) string {
	switch i {
	case models.IntentDeleteAccount, models.IntentDeleteCategory, models.IntentDeleteTransaction:
		return "SİLİNECEK"
	default:
		return "DEĞİŞTİRİLECEK"
	}
}
