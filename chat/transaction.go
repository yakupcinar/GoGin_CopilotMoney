package chat

import (
	"GoGinMoneyCopilot/models"
	"fmt"
	"time"
)

// Tarih penceresi — hem oluşturmada hem güncellemede geçerli.
//
// GERÇEK GÖZLEMLENEN HATA: model "geçen salı"yı 2024-07-16 olarak çözdü
// (doğrusu 2026-07-14). Prompt'ta bugünün tarihi verilmiş olmasına rağmen
// kendi eğitim verisindeki tarihe kaydı. Geniş bir pencere (5 yıl) bunu
// yakalayamamıştı — kullanıcının göremeyeceği sessiz bir veri bozulması.
const (
	maxPastDays   = 400 // ~13 ay
	maxFutureDays = 30
)

// dateInWindow — tarih makul aralıkta mı.
func dateInWindow(d, today time.Time) bool {
	return !d.After(today.AddDate(0, 0, maxFutureDays)) &&
		!d.Before(today.AddDate(0, 0, -maxPastDays))
}

// buildTransaction — create_transaction niyetinin doğrulaması.
//
// GÜVEN SINIRI: modelin ürettiği her alan burada süzülür. İki kademe var:
//
//	DÜZELTİLEMEZ  -> taslağı reddet (tutar, tip)
//	                 Bir para değerini uydurmak, kullanıcıya sormaktan kötüdür.
//	DÜZELTİLEBİLİR -> alanı temizle + kullanıcıya bildir (kategori, tarih, açıklama)
//	                 Kategoriyi düşürmek her şeyi çöpe atmaktan iyidir.
func (s *ActionService) buildTransaction(a *models.ParsedAction, req ChatRequest,
	categories []models.Category, today time.Time) (*models.CreateTransactionInput, []string, []string, error) {

	p := a.Params
	var warnings, needsInput []string

	// --- düzeltilemez: tutar ---
	// Model tutar bulamazsa 0 yazar (alan zorunlu, bir sayı yazmak zorunda).
	// Gerçekte gözlemlendi: "bugün markete gittim" -> amount: 0
	if p.Amount <= 0 {
		return nil, warnings, needsInput, fmt.Errorf("could not read the amount (%v), please specify it", p.Amount)
	}

	// --- düzeltilemez: tip ---
	if p.Type != "income" && p.Type != "expense" {
		return nil, warnings, needsInput, fmt.Errorf("invalid transaction type (%q)", p.Type)
	}

	// --- hesap: MODELDEN DEĞİL, istekten ---
	// Model çıktısında account_id diye bir alan yok; olsa da okumazdık.
	acc, err := s.resolveAccount(p, req)
	if err != nil {
		return nil, warnings, needsInput, fmt.Errorf("could not determine the account: %w", err)
	}

	// --- açıklama: kırp ---
	desc := p.Description
	if r := []rune(desc); len(r) > 100 {
		desc = string(r[:100])
		warnings = append(warnings, "description truncated to 100 characters")
	}

	// --- kategori: BEYAZ LİSTE + tip uyumu ---
	categoryID := 0
	if p.CategoryID != nil {
		var matched *models.Category
		for i := range categories {
			if categories[i].ID == *p.CategoryID {
				matched = &categories[i]
				break
			}
		}
		switch {
		case matched == nil:
			warnings = append(warnings, fmt.Sprintf(
				"the model suggested a category not in the list (id=%d), ignored", *p.CategoryID))
		case matched.Type != p.Type:
			warnings = append(warnings, fmt.Sprintf(
				"category %q (%s) does not match the transaction type (%s), ignored",
				matched.Name, matched.Type, p.Type))
		default:
			categoryID = matched.ID
		}
	}
	if categoryID == 0 {
		needsInput = append(needsInput, "category_id")
	}

	// --- tarih penceresi ---
	// GERÇEK GÖZLEMLENEN HATA: model "geçen salı"yı 2024-07-16 olarak çözdü
	// (doğrusu 2026-07-14). Prompt'ta bugünün tarihi verilmiş olmasına rağmen
	// kendi eğitim verisindeki tarihe kaydı. Geniş bir pencere (5 yıl) bunu
	// yakalayamamıştı — kullanıcının göremeyeceği sessiz bir veri bozulması.
	//
	// Kişisel finans girişlerinin ezici çoğunluğu son birkaç ay içindedir.
	// Bu pencerenin dışı şüphelidir: bugüne çekilir ve kullanıcıya bildirilir.
	date, err := time.Parse("2006-01-02", p.TransactionDate)
	switch {
	case err != nil:
		date = today
		warnings = append(warnings, fmt.Sprintf(
			"could not parse the date (%q), used today", p.TransactionDate))
	case date.After(today.AddDate(0, 0, maxFutureDays)):
		warnings = append(warnings, fmt.Sprintf(
			"date is too far in the future (%s), used today", date.Format("2006-01-02")))
		date = today
	case date.Before(today.AddDate(0, 0, -maxPastDays)):
		warnings = append(warnings, fmt.Sprintf(
			"date is too far in the past (%s) — the model may have produced the wrong year, used today",
			date.Format("2006-01-02")))
		date = today
	}

	// Eksik alan varsa payload üretme; kullanıcı tamamlasın.
	if len(needsInput) > 0 {
		return nil, warnings, needsInput, nil
	}

	return &models.CreateTransactionInput{
		AccountID:       acc.ID, // ← modelden DEĞİL
		CategoryID:      categoryID,
		Amount:          p.Amount,
		Type:            p.Type,
		Description:     desc,
		TransactionDate: date,
	}, warnings, needsInput, nil
}
