package chat

import (
	"GoGinMoneyCopilot/models"
	"GoGinMoneyCopilot/repositories"
	"errors"
	"strings"
)

// Hedef çözümleme.
//
// TEMEL KURAL: silme/değiştirmede model ID VERMEZ. Kullanıcının kullandığı
// ifadeyi ("balık", "Ana Hesap") target_ref alanına yazar; ID'yi biz buluruz.
// Böylece modelin uydurduğu bir ID ile yanlış kaydı silme riski yapısal
// olarak ortadan kalkar.
//
// İkinci kural: birden fazla aday varsa TAHMİN ETME, reddet. Yanlış kaydı
// silmek geri alınamaz; kullanıcıya sormak her zaman daha ucuzdur.

var (
	ErrAmbiguousTarget = errors.New("multiple matches found, please specify which one")
	ErrGlobalCategory  = errors.New("global categories cannot be modified via chat")
	ErrTransactionRef  = errors.New("the transaction is ambiguous, a transaction id is required")

	ErrNoAccount = errors.New("you must create an account first")
	// Birden fazla hesap varsa hangisine yazılacağı belirsizdir.
	ErrAccountAmbiguous = errors.New("you have multiple accounts, please specify which one to use")
)

// resolveAccount — sıra: açık id > isim > varsayılan hesap.
// Her yolda sahiplik SORGU seviyesinde doğrulanır.
func (s *ActionService) resolveAccount(p models.ActionParams, req ChatRequest) (*models.Account, error) {
	if p.TargetID != nil {
		return s.accounts.GetByIDForUser(*p.TargetID, req.UserID)
	}
	if strings.TrimSpace(p.TargetRef) != "" {
		accounts, err := s.accounts.ListForUser(req.UserID)
		if err != nil {
			return nil, err
		}
		return matchAccount(accounts, p.TargetRef)
	}
	if req.DefaultAccountID != 0 {
		return s.accounts.GetByIDForUser(req.DefaultAccountID, req.UserID)
	}

	// Hesap belirtilmemiş: kullanıcının TEK hesabı varsa onu kullan.
	// Birden fazlaysa tahmin etme — yanlış hesaba kayıt yazmak, kullanıcıya
	// sormaktan kötüdür (kategori/tutar mantığının aynısı).
	accounts, err := s.accounts.ListForUser(req.UserID)
	if err != nil {
		return nil, err
	}
	switch len(accounts) {
	case 0:
		return nil, ErrNoAccount
	case 1:
		return &accounts[0], nil
	default:
		return nil, ErrAccountAmbiguous
	}
}

// resolveCategory — global kategoriler (UserID == nil) chat üzerinden
// değiştirilemez: onlar tüm kullanıcılar tarafından paylaşılıyor.
func (s *ActionService) resolveCategory(p models.ActionParams, req ChatRequest) (*models.Category, error) {
	if p.TargetID != nil {
		cat, err := s.categories.GetByID(*p.TargetID)
		if err != nil {
			return nil, err
		}
		if cat.UserID == nil {
			return nil, ErrGlobalCategory
		}
		// Başkasının kategorisi: varlığını bile sızdırmadan "bulunamadı".
		if *cat.UserID != req.UserID {
			return nil, repositories.ErrCategoryNotFound
		}
		return cat, nil
	}

	categories, err := s.categories.GetForUser(req.UserID)
	if err != nil {
		return nil, err
	}
	cat, err := matchCategory(categories, p.TargetRef)
	if err != nil {
		return nil, err
	}
	if cat.UserID == nil {
		return nil, ErrGlobalCategory
	}
	return cat, nil
}

// resolveTransaction — işlemler isimle güvenilir çözülemez; açık id şart.
func (s *ActionService) resolveTransaction(p models.ActionParams, req ChatRequest) (*models.Transaction, error) {
	if p.TargetID == nil {
		return nil, ErrTransactionRef
	}
	tx, err := s.txs.GetByID(*p.TargetID)
	if err != nil {
		return nil, err
	}
	// İşlemin sahipliği, bağlı olduğu HESABIN sahipliğinden gelir.
	if _, err := s.accounts.GetByIDForUser(tx.AccountID, req.UserID); err != nil {
		return nil, repositories.ErrTransactionNotFound
	}
	return tx, nil
}

// ---------------------------------------------------------------------------
// İsimden eşleştirme: önce tam eşleşme, sonra "içeriyor".
// Birden fazla aday varsa ErrAmbiguousTarget.
// ---------------------------------------------------------------------------

func matchCategory(categories []models.Category, ref string) (*models.Category, error) {
	needle := strings.ToLower(strings.TrimSpace(ref))
	if needle == "" {
		return nil, repositories.ErrCategoryNotFound
	}

	var exact, partial []models.Category
	for _, c := range categories {
		name := strings.ToLower(c.Name)
		switch {
		case name == needle:
			exact = append(exact, c)
		case strings.Contains(name, needle) || strings.Contains(needle, name):
			partial = append(partial, c)
		}
	}
	pick := exact
	if len(pick) == 0 {
		pick = partial
	}
	switch len(pick) {
	case 0:
		return nil, repositories.ErrCategoryNotFound
	case 1:
		return &pick[0], nil
	default:
		return nil, ErrAmbiguousTarget
	}
}

func matchAccount(accounts []models.Account, ref string) (*models.Account, error) {
	needle := strings.ToLower(strings.TrimSpace(ref))
	if needle == "" {
		return nil, repositories.ErrAccountNotFound
	}

	var exact, partial []models.Account
	for _, a := range accounts {
		name := strings.ToLower(a.Name)
		switch {
		case name == needle:
			exact = append(exact, a)
		case strings.Contains(name, needle) || strings.Contains(needle, name):
			partial = append(partial, a)
		}
	}
	pick := exact
	if len(pick) == 0 {
		pick = partial
	}
	switch len(pick) {
	case 0:
		return nil, repositories.ErrAccountNotFound
	case 1:
		return &pick[0], nil
	default:
		return nil, ErrAmbiguousTarget
	}
}

// findCategoryByName — sadece "zaten var mı" uyarısı için; hata döndürmez.
func findCategoryByName(categories []models.Category, name string) *models.Category {
	needle := strings.ToLower(strings.TrimSpace(name))
	for i := range categories {
		if strings.ToLower(categories[i].Name) == needle {
			return &categories[i]
		}
	}
	return nil
}
