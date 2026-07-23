package chat

import (
	"GoGinMoneyCopilot/models"
	"GoGinMoneyCopilot/repositories"
	"fmt"
	"time"
)

// Confirm — frontend'deki "Evet, eminim" butonunun arkasındaki adım.
//
// İKİ AŞAMALI DOĞRULAMA:
//
//  1. Token doğrulama (repository'de, ATOMİK):
//     tek kullanımlık mı, süresi geçmiş mi, bu kullanıcıya mı ait.
//
//  2. Hedef doğrulama (BURADA, yeniden):
//     kayıt hâlâ var mı, hâlâ bu kullanıcıya mı ait, hâlâ silinebilir mi.
//
// İkincisi neden gerekli? Token üretildikten sonra dünya değişmiş olabilir:
// kayıt silinmiş, kategoriye yeni işlem eklenmiş, sahiplik değişmiş olabilir.
// Buna TOCTOU denir (kontrol anı ile kullanım anı arasındaki boşluk).
// Playground'da bunu canlı gördük: token geçerliydi ama kategori bu arada
// kullanıma girmişti, silme engellendi.
func (s *ActionService) Confirm(userID int, token string) (string, error) {
	action, err := s.pending.Claim(userID, token, time.Now())
	if err != nil {
		return "", err
	}

	switch action.Intent {
	case models.IntentDeleteCategory:
		return s.confirmDeleteCategory(userID, action)
	case models.IntentUpdateCategory:
		return s.confirmUpdateCategory(userID, action)
	case models.IntentDeleteAccount:
		return s.confirmDeleteAccount(userID, action)
	case models.IntentUpdateAccount:
		return s.confirmUpdateAccount(userID, action)
	case models.IntentDeleteTransaction:
		return s.confirmDeleteTransaction(userID, action)
	case models.IntentBudgetDelete:
		return s.confirmDeleteBudget(userID, action)
	case models.IntentBudgetUpdate:
		return s.confirmUpdateBudget(userID, action)
	case models.IntentUpdateTransaction:
		return s.confirmUpdateTransaction(userID, action)
	default:
		return "", fmt.Errorf("this action cannot be run via confirmation: %q", action.Intent)
	}
}

// --- kategori ---

func (s *ActionService) confirmDeleteCategory(userID int, a *models.PendingAction) (string, error) {
	cat, err := s.ownedCategory(userID, a.TargetID)
	if err != nil {
		return "", err
	}
	// Yeniden kontrol: bu arada kullanıma girmiş olabilir.
	used, err := s.txs.CountByCategory(cat.ID)
	if err != nil {
		return "", err
	}
	if used > 0 {
		return "", fmt.Errorf("%w (in %d transactions)", repositories.ErrCategoryInUse, used)
	}
	if err := s.categories.Delete(cat.ID); err != nil {
		return "", err
	}
	return fmt.Sprintf("category %q deleted", cat.Name), nil
}

func (s *ActionService) confirmUpdateCategory(userID int, a *models.PendingAction) (string, error) {
	cat, err := s.ownedCategory(userID, a.TargetID)
	if err != nil {
		return "", err
	}
	// Sadece verilen alanlar değişsin; boş bırakılanlar korunsun.
	name, catType := cat.Name, cat.Type
	if a.Params.Name != "" {
		name = a.Params.Name
	}
	if a.Params.CategoryType == "income" || a.Params.CategoryType == "expense" {
		catType = a.Params.CategoryType
	}
	if err := s.categories.Update(cat.ID, name, catType); err != nil {
		return "", err
	}
	return fmt.Sprintf("category updated: %q (%s)", name, catType), nil
}

// ownedCategory — kaydı çeker ve GERÇEKTEN bu kullanıcının kişisel
// kategorisi olduğunu doğrular. Global kategoriler değiştirilemez.
func (s *ActionService) ownedCategory(userID, categoryID int) (*models.Category, error) {
	cat, err := s.categories.GetByID(categoryID)
	if err != nil {
		return nil, err
	}
	if cat.UserID == nil {
		return nil, ErrGlobalCategory
	}
	if *cat.UserID != userID {
		return nil, repositories.ErrCategoryNotFound
	}
	return cat, nil
}

// --- hesap ---

func (s *ActionService) confirmDeleteAccount(userID int, a *models.PendingAction) (string, error) {
	acc, err := s.accounts.GetByIDForUser(a.TargetID, userID)
	if err != nil {
		return "", err
	}
	txs, err := s.txs.ListByAccount(acc.ID)
	if err != nil {
		return "", err
	}
	if len(txs) > 0 {
		return "", fmt.Errorf("%w (%d transactions)", repositories.ErrAccountInUse, len(txs))
	}
	if err := s.accounts.Delete(acc.ID); err != nil {
		return "", err
	}
	return fmt.Sprintf("account %q deleted", acc.Name), nil
}

func (s *ActionService) confirmUpdateAccount(userID int, a *models.PendingAction) (string, error) {
	acc, err := s.accounts.GetByIDForUser(a.TargetID, userID)
	if err != nil {
		return "", err
	}
	if a.Params.Name == "" {
		return "", validationErrorf("no new name provided")
	}
	if err := s.accounts.Update(acc.ID, a.Params.Name); err != nil {
		return "", err
	}
	return fmt.Sprintf("account name updated to %q", a.Params.Name), nil
}

// --- işlem ---

func (s *ActionService) confirmDeleteTransaction(userID int, a *models.PendingAction) (string, error) {
	tx, err := s.txs.GetByID(a.TargetID)
	if err != nil {
		return "", err
	}
	// İşlemin sahipliği bağlı olduğu hesaptan gelir — yeniden doğrula.
	if _, err := s.accounts.GetByIDForUser(tx.AccountID, userID); err != nil {
		return "", repositories.ErrTransactionNotFound
	}
	if err := s.txs.Delete(tx.ID); err != nil {
		return "", err
	}
	return fmt.Sprintf("transaction of %.2f TL %q deleted", tx.Amount, tx.Description), nil
}

// confirmUpdateTransaction — KISMİ güncelleme.
//
// TransactionRepository.Update beş alanın HEPSİNİ yazar. Kullanıcı
// "7 numaralı işlemi 60 tl yap" dediğinde model yalnızca amount üretir;
// diğerlerini boş göndersek açıklama silinir, kategori 0 olur, tarih sıfırlanır.
// O yüzden mevcut kaydın üstüne SADECE verilen alanları bindiriyoruz.
//
// "Verildi mi" ayrımı sıfır değerden: Amount==0 zaten geçersiz, CategoryID
// pointer (nil = verilmedi), Type/TransactionDate boş string.
// Bilinen sınır: açıklamayı KASTEN boşaltmak mümkün değil.
//
// OLUŞTURMADAN FARKI: orada kategori uyumsuzsa alanı düşürüp kullanıcıya
// sorabiliyorduk. Burada category_id NOT NULL ve zaten bir değeri var —
// düşürecek yer yok. Bu yüzden kategori sorunları SERT HATA.
func (s *ActionService) confirmUpdateTransaction(userID int, a *models.PendingAction) (string, error) {
	tx, err := s.txs.GetByID(a.TargetID)
	if err != nil {
		return "", err
	}
	// Sahiplik işlemin bağlı olduğu hesaptan gelir — yeniden doğrula (TOCTOU).
	if _, err := s.accounts.GetByIDForUser(tx.AccountID, userID); err != nil {
		return "", repositories.ErrTransactionNotFound
	}

	// Mevcut değerlerden başla.
	merged := models.UpdateTransactionInput{
		CategoryID:      tx.CategoryID,
		Amount:          tx.Amount,
		Type:            tx.Type,
		Description:     tx.Description,
		TransactionDate: tx.TransactionDate,
	}

	p := a.Params
	if p.Amount > 0 {
		merged.Amount = p.Amount
	}
	if p.Type == "income" || p.Type == "expense" {
		merged.Type = p.Type
	}
	if p.Description != "" {
		desc := p.Description
		if r := []rune(desc); len(r) > 100 {
			desc = string(r[:100])
		}
		merged.Description = desc
	}
	if p.CategoryID != nil {
		merged.CategoryID = *p.CategoryID
	}
	if p.TransactionDate != "" {
		parsed, err := time.Parse("2006-01-02", p.TransactionDate)
		if err != nil {
			return "", validationErrorf("could not parse the date: %q", p.TransactionDate)
		}
		merged.TransactionDate = parsed
	}

	// --- birleşmiş sonucu YENİDEN doğrula ---

	if merged.Amount <= 0 {
		return "", validationErrorf("amount must be positive (%v)", merged.Amount)
	}
	if !dateInWindow(merged.TransactionDate, startOfDay(time.Now())) {
		return "", validationErrorf("date is outside the reasonable range: %s",
			merged.TransactionDate.Format("2006-01-02"))
	}

	// Kategori: beyaz liste + tip uyumu.
	// DİKKAT: yalnızca kategori değişmemiş olsa bile kontrol ediyoruz —
	// çünkü TİP değişmiş olabilir ve eski kategori artık uyumsuz olabilir
	// ("gider" işlemi "gelir"e çevrilirse Market kategorisi geçersizleşir).
	categories, err := s.categories.GetForUser(userID)
	if err != nil {
		return "", err
	}
	var matched *models.Category
	for i := range categories {
		if categories[i].ID == merged.CategoryID {
			matched = &categories[i]
			break
		}
	}
	if matched == nil {
		return "", validationErrorf("category not found (id=%d)", merged.CategoryID)
	}
	if matched.Type != merged.Type {
		return "", validationErrorf("category %q (%s) does not match the new transaction type (%s)",
			matched.Name, matched.Type, merged.Type)
	}

	if err := s.txs.Update(tx.ID, merged); err != nil {
		return "", err
	}
	return fmt.Sprintf("transaction updated: %.2f TL %q (%s)",
		merged.Amount, merged.Description, merged.TransactionDate.Format("2006-01-02")), nil
}

// --- bütçe ---

// confirmDeleteBudget — bütçe silmenin onay adımı.
//
// TOCTOU: token BELİRLİ bir bütçe için üretildi (TargetID). Bu arada kullanıcı
// bütçesini silip YENİSİNİ kurmuş olabilir; o zaman GetForUser farklı bir
// id döner. id eşleşmiyorsa token bayattır — yeni bütçeyi YANLIŞLIKLA silme.
func (s *ActionService) confirmDeleteBudget(userID int, a *models.PendingAction) (string, error) {
	budget, err := s.budgets.GetForUser(userID)
	if err != nil {
		return "", err
	}
	if budget.ID != a.TargetID {
		// Bütçe bu arada değişti: token'ın işaret ettiği bütçe artık yok.
		return "", repositories.ErrBudgetNotFound
	}
	if err := s.budgets.Delete(budget.ID); err != nil {
		return "", err
	}
	return fmt.Sprintf("budget %q deleted", budget.Name), nil
}

// confirmUpdateBudget — bütçe değiştirmenin onay adımı.
//
// buildBudgetUpdate'i GÜNCEL duruma karşı yeniden çalıştırır: token beklerken
// kategori silinmiş/eklenmiş olabilir, merge tazelenir. Sonra TargetID eşleşme
// kontrolü (bütçe bu arada silinip yenisi kurulduysa reddet), sonra Replace.
func (s *ActionService) confirmUpdateBudget(userID int, a *models.PendingAction) (string, error) {
	input, budget, summary, err := s.buildBudgetUpdate(userID, a.Params)
	if err != nil {
		return "", err
	}
	if budget.ID != a.TargetID {
		return "", repositories.ErrBudgetNotFound
	}
	if err := s.budgets.Replace(budget.ID, *input, budget.StartDate); err != nil {
		return "", err
	}
	return "budget updated: " + summary, nil
}
