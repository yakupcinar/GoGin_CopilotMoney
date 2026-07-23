package chat

import (
	"GoGinMoneyCopilot/ai"
	"GoGinMoneyCopilot/models"
	"GoGinMoneyCopilot/repositories"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// ActionService — chat'in arkasındaki karar katmanı.
//
// MİMARİ NOT: bu katman "chat'e özel" DEĞİLDİR. Panel (normal arayüz) de
// aynı doğrulama ve onay kurallarına tabi olmalı. Chat sadece ikinci bir
// ön kapı; kurallar burada TEK yerde yaşar. Onayı arayüze bıraksaydık iki
// kez yazmak gerekir ve zamanla ayrışırlardı.
type ActionService struct {
	parser     ai.ActionParser
	accounts   repositories.AccountRepository
	categories repositories.CategoryRepository
	txs        repositories.TransactionRepository
	budgets    repositories.BudgetRepository
	pending    repositories.PendingActionRepository
}

func NewActionService(
	parser ai.ActionParser,
	accounts repositories.AccountRepository,
	categories repositories.CategoryRepository,
	txs repositories.TransactionRepository,
	budgets repositories.BudgetRepository,
	pending repositories.PendingActionRepository,
) *ActionService {
	return &ActionService{
		parser: parser, accounts: accounts, categories: categories,
		txs: txs, budgets: budgets, pending: pending,
	}
}

// ChatRequest — UserID ve Role JWT'den, DefaultAccountID istekten gelir.
// HİÇBİRİ modelden gelmez.
type ChatRequest struct {
	UserID           int
	Role             models.Role
	DefaultAccountID int // işlem oluştururken hesap belirtilmemişse kullanılır
	Text             string
}

var (
	ErrEmptyText   = errors.New("metin boş")
	ErrTextTooLong = errors.New("metin çok uzun")
)

// ValidationError — KULLANICIYA GÖSTERİLEBİLİR doğrulama hatası.
//
// Neden ayrı bir tip? Handler'ın "bu bir sunucu arızası mı, yoksa isteğin
// kendisi mi geçersiz" ayrımını yapabilmesi için. Jenerik fmt.Errorf
// döndürseydik handler onu tanıyamaz, 500 dönerdi — kullanıcı "sunucu bozuk"
// sanır, halbuki sorun kendi isteğinde ve düzeltebilir.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func validationErrorf(format string, a ...any) error {
	return &ValidationError{Msg: fmt.Sprintf(format, a...)}
}

const (
	maxTextLength = 500
	pendingTTL    = 5 * time.Minute
)

// Result — tek bir eylemin sonucu.
type Result struct {
	Intent models.Intent `json:"intent"`
	Risk   models.Risk   `json:"risk"`

	Data    any `json:"data,omitempty"`    // okuma sonucu
	Payload any `json:"payload,omitempty"` // oluşturma taslağı (POST gövdesi)

	RequiresConfirmation bool   `json:"requires_confirmation,omitempty"`
	Token                string `json:"token,omitempty"`
	Summary              string `json:"summary,omitempty"`

	// Extracted — payload üretilemese bile modelin ANLADIKLARI.
	// Kullanıcı sadece eksiği doldursun diye; her şeyi yeniden yazmasın.
	Extracted  *models.ActionParams `json:"extracted,omitempty"`
	Confidence float64              `json:"confidence"`
	Warnings   []string             `json:"warnings,omitempty"`
	NeedsInput []string             `json:"needs_input,omitempty"`
	Error      string               `json:"error,omitempty"`
}

// Chat — metni eylemlere çevirir ve her birini riskine göre işler.
func (s *ActionService) Chat(ctx context.Context, req ChatRequest) ([]Result, error) {
	// Girdi sınırı AI'a gitmeden önce: uzun metin = boşuna token = boşuna para.
	if len([]rune(req.Text)) == 0 {
		return nil, ErrEmptyText
	}
	if len([]rune(req.Text)) > maxTextLength {
		return nil, ErrTextTooLong
	}

	// İşlem tarihleri gün hassasiyetinde (DB'de DATE kolonu).
	today := startOfDay(time.Now())

	categories, err := s.categories.GetForUser(req.UserID)
	if err != nil {
		return nil, fmt.Errorf("kategoriler alınamadı: %w", err)
	}
	accounts, err := s.accounts.ListForUser(req.UserID)
	if err != nil {
		return nil, fmt.Errorf("hesaplar alınamadı: %w", err)
	}

	actions, err := s.parser.Parse(ctx, ai.ParseInput{
		Text:       req.Text,
		Categories: categories,
		Accounts:   accounts,
		Today:      today,
	})
	if err != nil {
		return nil, fmt.Errorf("ayrıştırma başarısız: %w", err)
	}

	results := make([]Result, 0, len(actions))
	for i := range actions {
		results = append(results, s.handle(&actions[i], req, categories, today))
	}
	return results, nil
}

// handle — tek eylemi doğrular ve riskine göre sonuç üretir.
func (s *ActionService) handle(a *models.ParsedAction, req ChatRequest,
	categories []models.Category, today time.Time) Result {

	// BEYAZ LİSTE: modelin uydurduğu bir niyet asla çalışmaz.
	risk, ok := models.RiskOf(a.Intent)
	if !ok {
		return Result{
			Intent: a.Intent, Confidence: a.Confidence,
			Error: fmt.Sprintf("bilinmeyen veya izin verilmeyen işlem: %q", a.Intent),
		}
	}

	res := Result{
		Intent: a.Intent, Risk: risk,
		Extracted:  &a.Params,
		Confidence: a.Confidence,
		Warnings:   append([]string{}, a.Warnings...),
	}

	switch a.Intent {

	// ---------------- okuma: doğrudan çalıştır ----------------
	case models.IntentListCategories:
		res.Data = categories

	case models.IntentGetAccount:
		acc, err := s.resolveAccount(a.Params, req)
		if err != nil {
			res.Error = err.Error()
			return res
		}
		res.Data = acc

	case models.IntentListTransactions:
		acc, err := s.resolveAccount(a.Params, req)
		if err != nil {
			res.Error = err.Error()
			return res
		}
		txs, err := s.txs.ListByAccount(acc.ID)
		if err != nil {
			res.Error = "işlemler alınamadı"
			return res
		}
		res.Data = txs

	case models.IntentGetTransaction:
		tx, err := s.resolveTransaction(a.Params, req)
		if err != nil {
			res.Error = err.Error()
			return res
		}
		res.Data = tx

	case models.IntentBudgetView:
		// Görünüm mantığı HTTP handler'ıyla ORTAK (chat.BuildBudgetView).
		// Chat şimdilik yalnızca içinde bulunulan dönemi gösterir (offset 0);
		// "geçen ayki bütçem" gibi göreli dönem ileride eklenebilir.
		now := time.Now().In(models.AppLocation())
		view, err := BuildBudgetView(s.budgets, s.categories, s.accounts, s.txs, req.UserID, 0, now)
		if err != nil {
			if errors.Is(err, repositories.ErrBudgetNotFound) {
				res.Error = "henüz bir bütçeniz yok"
				return res
			}
			// Altyapı hatası: detayı sızdırma, jenerik mesaj.
			res.Error = "bütçe alınamadı"
			return res
		}
		res.Data = view

	// ---------------- oluşturma: taslak üret ----------------
	case models.IntentCreateAccount:
		if a.Params.Name == "" {
			res.NeedsInput = append(res.NeedsInput, "name")
			return res
		}
		res.Payload = models.CreateAccountInput{Name: a.Params.Name}

	case models.IntentCreateCategory:
		if a.Params.Name == "" {
			res.NeedsInput = append(res.NeedsInput, "name")
		}
		if a.Params.CategoryType != "income" && a.Params.CategoryType != "expense" {
			res.NeedsInput = append(res.NeedsInput, "category_type")
		}
		if len(res.NeedsInput) > 0 {
			return res
		}
		// Aynı isimde kategori zaten varsa kullanıcıyı uyar (engelleme).
		if existing := findCategoryByName(categories, a.Params.Name); existing != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf(
				"%q adında bir kategori zaten var (id=%d)", existing.Name, existing.ID))
		}
		res.Payload = models.CreateCategoryInput{
			Name: a.Params.Name, Type: a.Params.CategoryType,
		}

	case models.IntentCreateTransaction:
		payload, warns, needs, err := s.buildTransaction(a, req, categories, today)
		res.Warnings = append(res.Warnings, warns...)
		res.NeedsInput = append(res.NeedsInput, needs...)
		if err != nil {
			res.Error = err.Error()
			return res
		}
		if payload != nil {
			res.Payload = *payload
		}

	case models.IntentBudgetSet:
		payload, warns, needs, err := s.buildBudget(a, req, categories, today)
		res.Warnings = append(res.Warnings, warns...)
		res.NeedsInput = append(res.NeedsInput, needs...)
		if err != nil {
			res.Error = err.Error()
			return res
		}
		if payload != nil {
			res.Payload = *payload
		}

	// ---------------- yıkıcı: token + açık onay ----------------
	case models.IntentDeleteCategory, models.IntentUpdateCategory:
		s.prepareCategoryAction(&res, a, req, categories)

	case models.IntentDeleteAccount, models.IntentUpdateAccount:
		s.prepareAccountAction(&res, a, req)

	case models.IntentDeleteTransaction, models.IntentUpdateTransaction:
		s.prepareTransactionAction(&res, a, req)

	default:
		res.Error = "işlem anlaşılamadı"
	}

	return res
}

// attachConfirmation — onay kaydı oluşturur ve token'ı sonuca ekler.
func (s *ActionService) attachConfirmation(res *Result, userID int,
	intent models.Intent, targetID int, summary string, params models.ActionParams) {

	token, err := newToken()
	if err != nil {
		res.Error = "onay kodu üretilemedi"
		return
	}
	action := &models.PendingAction{
		Token: token, UserID: userID, Intent: intent, TargetID: targetID,
		Summary: summary, Params: params,
		ExpiresAt: time.Now().Add(pendingTTL),
	}
	if err := s.pending.Create(action); err != nil {
		res.Error = "onay kodu kaydedilemedi"
		return
	}

	res.RequiresConfirmation = true
	res.Token = token
	res.Summary = summary
}

func newToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "act_" + hex.EncodeToString(b), nil
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}
