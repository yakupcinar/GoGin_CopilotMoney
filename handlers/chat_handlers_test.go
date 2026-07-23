package handlers

import (
	"GoGinMoneyCopilot/chat"
	"GoGinMoneyCopilot/models"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// Chat akışının testleri.
//
// KAPSAM: handler + chat.ActionService BİRLİKTE test ediliyor. Yalnızca AI
// sahte — repository'ler de sahte ama gerçek servis mantığından geçiliyor.
// Yani beyaz liste, sahiplik kontrolü, doğrulama katmanı ve onay akışının
// tamamı gerçekten çalışıyor.
//
// Modelin çıktısını tam kontrol edebildiğimiz için "model saçmalarsa ne olur"
// senaryolarını deterministik biçimde test edebiliyoruz.

type chatFixture struct {
	router     *gin.Engine
	parser     *fakeActionParser
	accounts   *fakeAccountRepo
	categories *fakeCategoryRepo
	txs        *fakeTransactionRepo
	budgets    *fakeBudgetRepo
	pending    *fakePendingRepo
}

const (
	chatUserID    = 1
	chatAccountID = 10
	otherUserID   = 2
	otherAcctID   = 20
)

func newChatFixture(t *testing.T, actions ...models.ParsedAction) *chatFixture {
	t.Helper()

	parser := &fakeActionParser{actions: actions}
	accounts := newFakeAccountRepo()
	accounts.seed(&models.Account{ID: chatAccountID, Name: "Ana Hesap", UserID: chatUserID})
	accounts.seed(&models.Account{ID: otherAcctID, Name: "Baskasi", UserID: otherUserID})

	uid := chatUserID
	categories := newFakeCategoryRepo()
	categories.seed(&models.Category{ID: 1, Name: "Yeme", Type: "expense", UserID: &uid})
	categories.seed(&models.Category{ID: 2, Name: "Bos Kategori", Type: "expense", UserID: &uid})
	categories.seed(&models.Category{ID: 3, Name: "Global", Type: "expense", UserID: nil})

	txs := newFakeTransactionRepo()
	txs.seed(&models.Transaction{
		ID: 100, AccountID: chatAccountID, CategoryID: 1, Amount: 50,
		Type: "expense", Description: "kahve", TransactionDate: time.Now(),
	})

	budgets := newFakeBudgetRepo()
	pending := newFakePendingRepo()
	svc := chat.NewActionService(parser, accounts, categories, txs, budgets, pending)
	h := NewChatHandler(svc)

	r := gin.New()
	r.Use(authAs(chatUserID, models.RoleClient))
	r.POST("/chat", h.Chat)
	r.POST("/actions/confirm", h.Confirm)

	return &chatFixture{r, parser, accounts, categories, txs, budgets, pending}
}

// firstResult — cevaptaki ilk eylemi çözer.
func firstResult(t *testing.T, w *httptest.ResponseRecorder) chat.Result {
	t.Helper()
	var body struct {
		Results []chat.Result `json:"results"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("cevap parse edilemedi: %v (%s)", err, w.Body.String())
	}
	if len(body.Results) == 0 {
		t.Fatalf("sonuç yok: %s", w.Body.String())
	}
	return body.Results[0]
}

func txAction(params models.ActionParams) models.ParsedAction {
	return models.ParsedAction{
		Intent: models.IntentCreateTransaction, Params: params, Confidence: 0.9,
	}
}

// ---------------------------------------------------------------------------
// Girdi sınırları — AI'a GİTMEDEN önce
// ---------------------------------------------------------------------------

func TestChat_EmptyText_Returns400(t *testing.T) {
	f := newChatFixture(t)

	w := performRequest(f.router, "POST", "/chat", `{"text":""}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("beklenen 400, gelen %d", w.Code)
	}
	if f.parser.calls != 0 {
		t.Fatal("boş metin için AI çağrıldı — boşuna token harcanıyor")
	}
}

func TestChat_TooLongText_RejectedBeforeAICall(t *testing.T) {
	f := newChatFixture(t)

	w := performRequest(f.router, "POST", "/chat",
		`{"text":"`+strings.Repeat("a", 600)+`"}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("beklenen 400, gelen %d", w.Code)
	}
	if f.parser.calls != 0 {
		t.Fatal("çok uzun metin için AI çağrıldı")
	}
}

// AI servisi çökerse 500 DEĞİL 503: bu bizim hatamız değil, dış bağımlılığın.
func TestChat_ParserFailure_Returns503(t *testing.T) {
	f := newChatFixture(t)
	f.parser.err = errors.New("groq erişilemiyor")

	w := performRequest(f.router, "POST", "/chat", `{"text":"kahve 50 tl"}`)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("beklenen 503, gelen %d (%s)", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "groq") {
		t.Fatal("iç hata detayı client'a sızdı")
	}
}

// GROQ_API_KEY yoksa servis nil olur — 404 değil, açıklayıcı 503.
func TestChat_ServiceNotConfigured_Returns503(t *testing.T) {
	h := NewChatHandler(nil)
	r := gin.New()
	r.Use(authAs(chatUserID, models.RoleClient))
	r.POST("/chat", h.Chat)

	w := performRequest(r, "POST", "/chat", `{"text":"kahve"}`)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("beklenen 503, gelen %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Beyaz liste — modelin uydurduğu niyet ASLA çalışmaz
// ---------------------------------------------------------------------------

func TestChat_UnknownIntent_Rejected(t *testing.T) {
	f := newChatFixture(t,
		models.ParsedAction{Intent: models.Intent("drop_all_tables"), Confidence: 1},
		models.ParsedAction{Intent: models.Intent("login_as_admin"), Confidence: 1},
	)

	w := performRequest(f.router, "POST", "/chat", `{"text":"herhangi bir şey"}`)

	var body struct {
		Results []chat.Result `json:"results"`
	}
	json.Unmarshal(w.Body.Bytes(), &body)
	if len(body.Results) != 2 {
		t.Fatalf("2 sonuç bekleniyordu, gelen %d", len(body.Results))
	}
	for i, r := range body.Results {
		if r.Error == "" {
			t.Fatalf("eylem %d reddedilmedi: %+v", i+1, r)
		}
		if r.Payload != nil || r.Token != "" {
			t.Fatalf("izinsiz niyet için payload/token üretildi: %+v", r)
		}
	}
}

// ---------------------------------------------------------------------------
// İşlem oluşturma — doğrulama katmanı
// ---------------------------------------------------------------------------

func TestChat_CreateTransaction_ProducesPayload(t *testing.T) {
	catID := 1
	f := newChatFixture(t, txAction(models.ActionParams{
		Amount: 50.5, Type: "expense", Description: "kahve",
		CategoryID: &catID, TransactionDate: time.Now().Format("2006-01-02"),
	}))

	w := performRequest(f.router, "POST", "/chat", `{"text":"kahve 50.5"}`)

	res := firstResult(t, w)
	if res.Error != "" {
		t.Fatalf("beklenmeyen hata: %s", res.Error)
	}
	payload, _ := json.Marshal(res.Payload)
	var input models.CreateTransactionInput
	json.Unmarshal(payload, &input)

	// account_id MODELDEN gelmiyor — istekten/varsayılandan geliyor.
	if input.AccountID != chatAccountID {
		t.Fatalf("account_id yanlış: %d", input.AccountID)
	}
	if input.Amount != 50.5 {
		t.Fatalf("tutar yanlış: %v", input.Amount)
	}
}

// Model tutar bulamazsa 0 yazar (alan zorunlu). Bu düzeltilemez -> reddet.
func TestChat_ZeroAmount_Rejected(t *testing.T) {
	catID := 1
	f := newChatFixture(t, txAction(models.ActionParams{
		Amount: 0, Type: "expense", Description: "market",
		CategoryID: &catID, TransactionDate: time.Now().Format("2006-01-02"),
	}))

	w := performRequest(f.router, "POST", "/chat", `{"text":"bugün markete gittim"}`)

	res := firstResult(t, w)
	if res.Error == "" {
		t.Fatal("tutar 0 iken taslak üretildi")
	}
	if res.Payload != nil {
		t.Fatal("geçersiz tutarla payload üretildi")
	}
}

// Model listede OLMAYAN kategori önerirse: düşür + uyar (reddetme).
func TestChat_UnknownCategory_DroppedWithWarning(t *testing.T) {
	bogus := 999
	f := newChatFixture(t, txAction(models.ActionParams{
		Amount: 50, Type: "expense", Description: "kahve",
		CategoryID: &bogus, TransactionDate: time.Now().Format("2006-01-02"),
	}))

	w := performRequest(f.router, "POST", "/chat", `{"text":"kahve 50"}`)

	res := firstResult(t, w)
	if res.Payload != nil {
		t.Fatal("uydurma kategoriyle payload üretildi")
	}
	if len(res.NeedsInput) == 0 {
		t.Fatal("category_id eksik olarak işaretlenmedi")
	}
	if len(res.Warnings) == 0 {
		t.Fatal("kullanıcıya uyarı verilmedi")
	}
}

// GERÇEK GÖZLEMLENEN HATA: model "geçen salı"yı 2024 olarak çözdü.
// Tarih penceresi bunu yakalamalı ve bugüne çekmeli.
func TestChat_StaleYear_ClampedToToday(t *testing.T) {
	catID := 1
	f := newChatFixture(t, txAction(models.ActionParams{
		Amount: 145, Type: "expense", Description: "taksi",
		CategoryID: &catID, TransactionDate: "2020-07-16", // çok geride
	}))

	w := performRequest(f.router, "POST", "/chat", `{"text":"geçen salı taksi 145"}`)

	res := firstResult(t, w)
	payload, _ := json.Marshal(res.Payload)
	var input models.CreateTransactionInput
	json.Unmarshal(payload, &input)

	if input.TransactionDate.Year() == 2020 {
		t.Fatal("eski yıl geçti — tarih penceresi çalışmıyor")
	}
	if len(res.Warnings) == 0 {
		t.Fatal("tarih düzeltildi ama kullanıcı bilgilendirilmedi")
	}
}

// ---------------------------------------------------------------------------
// Sahiplik — başkasının kaydına erişilemez
// ---------------------------------------------------------------------------

func TestChat_ForeignAccount_Rejected(t *testing.T) {
	other := otherAcctID
	f := newChatFixture(t, models.ParsedAction{
		Intent: models.IntentGetAccount,
		Params: models.ActionParams{TargetID: &other},
	})

	w := performRequest(f.router, "POST", "/chat", `{"text":"20 numaralı hesabı göster"}`)

	res := firstResult(t, w)
	if res.Error == "" {
		t.Fatal("başkasının hesabına erişim reddedilmedi")
	}
	if res.Data != nil {
		t.Fatal("başkasının hesap verisi sızdı")
	}
}

// Global kategoriler (UserID == nil) chat üzerinden değiştirilemez.
func TestChat_GlobalCategoryDelete_Rejected(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{
		Intent: models.IntentDeleteCategory,
		Params: models.ActionParams{TargetRef: "Global"},
	})

	w := performRequest(f.router, "POST", "/chat", `{"text":"global kategorisini sil"}`)

	res := firstResult(t, w)
	if res.Error == "" {
		t.Fatal("global kategori silme reddedilmedi")
	}
	if res.Token != "" {
		t.Fatal("global kategori için onay kodu üretildi")
	}
}

// ---------------------------------------------------------------------------
// Yıkıcı işlemler — token akışı
// ---------------------------------------------------------------------------

func TestChat_DeleteCategory_IssuesConfirmationToken(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{
		Intent: models.IntentDeleteCategory,
		Params: models.ActionParams{TargetRef: "Bos Kategori"},
	})

	w := performRequest(f.router, "POST", "/chat", `{"text":"bos kategori sil"}`)

	res := firstResult(t, w)
	if !res.RequiresConfirmation || res.Token == "" {
		t.Fatalf("onay kodu üretilmedi: %+v", res)
	}
	if res.Summary == "" {
		t.Fatal("özet yok — frontend popup'ta ne gösterecek?")
	}
	if !strings.Contains(res.Summary, "Bos Kategori") {
		t.Fatalf("özet hedefi belirtmiyor: %q", res.Summary)
	}
}

// Silinemeyecek bir şey için onay SORULMAMALI.
// Kullanıcı "Evet"e basıp sonra hata almasın.
func TestChat_DeleteCategoryInUse_NoTokenIssued(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{
		Intent: models.IntentDeleteCategory,
		Params: models.ActionParams{TargetRef: "Yeme"}, // 100 numaralı işlem kullanıyor
	})

	w := performRequest(f.router, "POST", "/chat", `{"text":"yeme kategorisini sil"}`)

	res := firstResult(t, w)
	if res.Token != "" {
		t.Fatal("kullanımdaki kategori için onay kodu üretildi")
	}
	if res.Error == "" {
		t.Fatal("kullanıcıya sebep bildirilmedi")
	}
}

func TestConfirm_ValidToken_ExecutesDeletion(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{
		Intent: models.IntentDeleteCategory,
		Params: models.ActionParams{TargetRef: "Bos Kategori"},
	})
	token := firstResult(t, performRequest(f.router, "POST", "/chat", `{"text":"sil"}`)).Token

	w := performRequest(f.router, "POST", "/actions/confirm",
		`{"token":"`+token+`"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("beklenen 200, gelen %d (%s)", w.Code, w.Body.String())
	}
	if _, err := f.categories.GetByID(2); err == nil {
		t.Fatal("kategori silinmedi")
	}
}

// Token TEK KULLANIMLIK.
func TestConfirm_ReusedToken_Rejected(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{
		Intent: models.IntentDeleteCategory,
		Params: models.ActionParams{TargetRef: "Bos Kategori"},
	})
	token := firstResult(t, performRequest(f.router, "POST", "/chat", `{"text":"sil"}`)).Token

	performRequest(f.router, "POST", "/actions/confirm", `{"token":"`+token+`"}`)
	second := performRequest(f.router, "POST", "/actions/confirm", `{"token":"`+token+`"}`)

	if second.Code != http.StatusBadRequest {
		t.Fatalf("ikinci kullanımda 400 bekleniyordu, gelen %d", second.Code)
	}
}

func TestConfirm_UnknownToken_Rejected(t *testing.T) {
	f := newChatFixture(t)

	w := performRequest(f.router, "POST", "/actions/confirm", `{"token":"act_uydurma"}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("beklenen 400, gelen %d", w.Code)
	}
}

// Başkasının token'ı kullanılamaz — ve sebep AYRIŞTIRILAMAZ olmalı
// (aksi halde token'ın varlığı sızar).
func TestConfirm_ForeignToken_RejectedIndistinguishably(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{
		Intent: models.IntentDeleteCategory,
		Params: models.ActionParams{TargetRef: "Bos Kategori"},
	})
	token := firstResult(t, performRequest(f.router, "POST", "/chat", `{"text":"sil"}`)).Token

	// Aynı servis, FARKLI kullanıcı.
	svc := chat.NewActionService(f.parser, f.accounts, f.categories, f.txs, f.budgets, f.pending)
	other := gin.New()
	other.Use(authAs(otherUserID, models.RoleClient))
	other.POST("/actions/confirm", NewChatHandler(svc).Confirm)

	foreign := performRequest(other, "POST", "/actions/confirm", `{"token":"`+token+`"}`)
	unknown := performRequest(other, "POST", "/actions/confirm", `{"token":"act_hicyok"}`)

	if foreign.Code != http.StatusBadRequest {
		t.Fatalf("başkasının token'ı kabul edildi: %d", foreign.Code)
	}
	if foreign.Body.String() != unknown.Body.String() {
		t.Fatalf("cevaplar ayrışıyor -> token varlığı sızıyor:\n  yabancı: %s\n  olmayan: %s",
			foreign.Body.String(), unknown.Body.String())
	}
}

// TOCTOU: token üretildikten SONRA kategori kullanıma girerse,
// onay anında yeniden kontrol edilip engellenmeli.
func TestConfirm_TargetBecameInUse_Blocked(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{
		Intent: models.IntentDeleteCategory,
		Params: models.ActionParams{TargetRef: "Bos Kategori"},
	})
	token := firstResult(t, performRequest(f.router, "POST", "/chat", `{"text":"sil"}`)).Token

	// Dünya değişti: kategoriye bir işlem eklendi.
	f.txs.seed(&models.Transaction{
		ID: 200, AccountID: chatAccountID, CategoryID: 2, Amount: 10,
		Type: "expense", Description: "yeni", TransactionDate: time.Now(),
	})

	w := performRequest(f.router, "POST", "/actions/confirm", `{"token":"`+token+`"}`)

	if w.Code != http.StatusConflict {
		t.Fatalf("beklenen 409, gelen %d (%s)", w.Code, w.Body.String())
	}
	if _, err := f.categories.GetByID(2); err != nil {
		t.Fatal("kategori silindi — TOCTOU koruması çalışmadı")
	}
}

// budget_view: chat üzerinden bütçe görüntüleme (okuma niyeti, onaysız).
//
// GÜVEN SINIRI: sahte parser IntentBudgetView döndürür (model bunu üretmiş
// gibi); gerçek olan chat.ActionService'in bu niyeti BuildBudgetView'e
// yönlendirmesi ve HTTP handler'ıyla AYNI sonucu üretmesidir.
func TestChat_BudgetView_ReturnsCurrentPeriod(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{Intent: models.IntentBudgetView})
	// Kategori 1 (Yeme) için 500 limitli, bugünü içeren bir bütçe.
	f.budgets.seed(
		&models.Budget{ID: 1, UserID: chatUserID, Name: "Aylık",
			StartDate: models.CivilDate(time.Now().AddDate(0, 0, -5)), PeriodDays: 30},
		[]models.BudgetCategory{{ID: 1, BudgetID: 1, CategoryID: 1, LimitAmount: 500}},
	)

	w := performRequest(f.router, "POST", "/chat", `{"text":"bütçemi göster"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("beklenen 200, gelen %d (body: %s)", w.Code, w.Body.String())
	}
	res := firstResult(t, w)
	if res.Error != "" {
		t.Fatalf("beklenmeyen hata: %s", res.Error)
	}
	if res.Risk != models.RiskRead {
		t.Fatalf("budget_view okuma niyeti olmalı, gelen risk: %q", res.Risk)
	}

	// res.Data JSON'a serialize edilmiş bir BudgetView; geri çözüp doğrula.
	raw, _ := json.Marshal(res.Data)
	var view models.BudgetView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("BudgetView çözülemedi: %v", err)
	}
	// Fixture'daki 50 TL'lik "kahve" işlemi (kategori 1) bu dönemde sayılmalı.
	if view.TotalSpent != 50 {
		t.Fatalf("harcama 50 beklendi, gelen %v", view.TotalSpent)
	}
	if view.TotalLimit != 500 {
		t.Fatalf("limit 500 beklendi, gelen %v", view.TotalLimit)
	}
}

// Bütçesi olmayan kullanıcı chat'ten bütçe isterse: 200 + anlaşılır hata,
// 500 DEĞİL (kullanıcı hatası, sunucu arızası değil).
func TestChat_BudgetView_NoBudget(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{Intent: models.IntentBudgetView})

	w := performRequest(f.router, "POST", "/chat", `{"text":"bütçemi göster"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("beklenen 200, gelen %d", w.Code)
	}
	res := firstResult(t, w)
	if res.Error != "you don't have a budget yet" {
		t.Fatalf("anlaşılır 'bütçe yok' mesajı beklendi, gelen: %q", res.Error)
	}
}

// budget_set: chat'ten bütçe kurma (create kademesi — YAZMAZ, taslak üretir).
//
// create_transaction ile aynı desen: sonuç res.Payload (bir CreateBudgetInput);
// frontend onu POST /budgets ile gönderir. Gerçek yazma REST kapısından geçer.
func TestChat_BudgetSet_ProducesDraft(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{
		Intent: models.IntentBudgetSet,
		Params: models.ActionParams{
			PeriodDays: 30,
			BudgetCategories: []models.BudgetCategoryParam{
				{CategoryRef: "Yeme", Amount: 500},
			},
		},
	})

	w := performRequest(f.router, "POST", "/chat", `{"text":"yemeye 500 aylık bütçe"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("beklenen 200, gelen %d (body: %s)", w.Code, w.Body.String())
	}
	res := firstResult(t, w)
	if res.Error != "" {
		t.Fatalf("beklenmeyen hata: %s", res.Error)
	}
	if res.Risk != models.RiskCreate {
		t.Fatalf("budget_set create kademesi olmalı, gelen: %q", res.Risk)
	}

	raw, _ := json.Marshal(res.Payload)
	var input models.CreateBudgetInput
	if err := json.Unmarshal(raw, &input); err != nil {
		t.Fatalf("CreateBudgetInput çözülemedi: %v", err)
	}
	if input.PeriodDays != 30 {
		t.Fatalf("period_days 30 beklendi, gelen %d", input.PeriodDays)
	}
	if len(input.Categories) != 1 || input.Categories[0].CategoryID != 1 || input.Categories[0].LimitAmount != 500 {
		t.Fatalf("kategori satırı yanlış çözüldü: %+v", input.Categories)
	}
	if input.StartDate != time.Now().Format(models.DateLayout) {
		t.Fatalf("başlangıç bugün olmalı, gelen %q", input.StartDate)
	}
}

// Dönem verilmezse: değer UYDURMA, kullanıcıdan iste.
func TestChat_BudgetSet_MissingPeriodNeedsInput(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{
		Intent: models.IntentBudgetSet,
		Params: models.ActionParams{
			BudgetCategories: []models.BudgetCategoryParam{{CategoryRef: "Yeme", Amount: 500}},
		},
	})
	res := firstResult(t, performRequest(f.router, "POST", "/chat", `{"text":"yemeye 500 bütçe"}`))
	if res.Payload != nil {
		t.Fatalf("eksik dönemle taslak üretilmemeli")
	}
	found := false
	for _, n := range res.NeedsInput {
		if n == "period_days" {
			found = true
		}
	}
	if !found {
		t.Fatalf("period_days NeedsInput'ta olmalı, gelen: %v", res.NeedsInput)
	}
}

// Bilinmeyen kategori: reddet (model id uyduramaz, ref çözülemedi).
func TestChat_BudgetSet_UnknownCategory(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{
		Intent: models.IntentBudgetSet,
		Params: models.ActionParams{
			PeriodDays:       30,
			BudgetCategories: []models.BudgetCategoryParam{{CategoryRef: "YokBöyle", Amount: 500}},
		},
	})
	res := firstResult(t, performRequest(f.router, "POST", "/chat", `{"text":"x"}`))
	if res.Error == "" {
		t.Fatalf("bilinmeyen kategori reddedilmeliydi")
	}
	if res.Payload != nil {
		t.Fatalf("hata varken taslak üretilmemeli")
	}
}

// Gelir kategorisi bütçelenemez.
func TestChat_BudgetSet_IncomeCategoryRejected(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{
		Intent: models.IntentBudgetSet,
		Params: models.ActionParams{
			PeriodDays:       30,
			BudgetCategories: []models.BudgetCategoryParam{{CategoryRef: "Maas", Amount: 500}},
		},
	})
	uid := chatUserID
	f.categories.seed(&models.Category{ID: 5, Name: "Maas", Type: "income", UserID: &uid})

	res := firstResult(t, performRequest(f.router, "POST", "/chat", `{"text":"x"}`))
	if res.Error == "" || res.Payload != nil {
		t.Fatalf("gelir kategorisi reddedilmeliydi (error: %q)", res.Error)
	}
}

// Aynı kategori iki kez verilirse reddet.
func TestChat_BudgetSet_DuplicateCategory(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{
		Intent: models.IntentBudgetSet,
		Params: models.ActionParams{
			PeriodDays: 30,
			BudgetCategories: []models.BudgetCategoryParam{
				{CategoryRef: "Yeme", Amount: 500},
				{CategoryRef: "Yeme", Amount: 200},
			},
		},
	})
	res := firstResult(t, performRequest(f.router, "POST", "/chat", `{"text":"x"}`))
	if res.Error == "" || res.Payload != nil {
		t.Fatalf("yinelenen kategori reddedilmeliydi")
	}
}

// Zaten bütçesi olan kullanıcı: create çakışırdı, anlaşılır mesaj ver.
func TestChat_BudgetSet_ExistingBudgetRejected(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{
		Intent: models.IntentBudgetSet,
		Params: models.ActionParams{
			PeriodDays:       30,
			BudgetCategories: []models.BudgetCategoryParam{{CategoryRef: "Yeme", Amount: 500}},
		},
	})
	f.budgets.seed(&models.Budget{ID: 1, UserID: chatUserID, Name: "Var",
		StartDate: models.CivilDate(time.Now()), PeriodDays: 30}, nil)

	res := firstResult(t, performRequest(f.router, "POST", "/chat", `{"text":"x"}`))
	if res.Payload != nil {
		t.Fatalf("bütçe varken taslak üretilmemeli")
	}
	if !strings.Contains(res.Error, "you already have a budget") {
		t.Fatalf("anlaşılır 'zaten var' mesajı beklendi, gelen: %q", res.Error)
	}
}

// budget_delete: yıkıcı niyet — ONAY İSTER, hemen silmez.
func TestChat_BudgetDelete_RequiresConfirmation(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{Intent: models.IntentBudgetDelete})
	f.budgets.seed(&models.Budget{ID: 1, UserID: chatUserID, Name: "Aylık",
		StartDate: models.CivilDate(time.Now()), PeriodDays: 30},
		[]models.BudgetCategory{{ID: 1, BudgetID: 1, CategoryID: 1, LimitAmount: 500}})

	res := firstResult(t, performRequest(f.router, "POST", "/chat", `{"text":"bütçemi sil"}`))
	if res.Risk != models.RiskDestructive {
		t.Fatalf("yıkıcı kademe beklendi, gelen: %q", res.Risk)
	}
	if !res.RequiresConfirmation || res.Token == "" {
		t.Fatalf("onay + token beklendi, gelen: %+v", res)
	}
	// Henüz SİLİNMEMELİ — sadece onay bekliyor.
	if len(f.budgets.budgets) != 1 {
		t.Fatalf("onaydan önce bütçe silinmemeli")
	}
}

// Bütçesi olmayan kullanıcı: token HİÇ üretilmez (boşuna "emin misin?" sorma).
func TestChat_BudgetDelete_NoBudget(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{Intent: models.IntentBudgetDelete})
	res := firstResult(t, performRequest(f.router, "POST", "/chat", `{"text":"bütçemi sil"}`))
	if res.RequiresConfirmation || res.Token != "" {
		t.Fatalf("bütçe yokken token üretilmemeli")
	}
	if res.Error != "you don't have a budget to delete" {
		t.Fatalf("anlaşılır mesaj beklendi, gelen: %q", res.Error)
	}
}

// Onay akışının tamamı: chat -> token -> confirm -> gerçekten silinir.
func TestConfirm_BudgetDelete_Deletes(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{Intent: models.IntentBudgetDelete})
	f.budgets.seed(&models.Budget{ID: 1, UserID: chatUserID, Name: "Aylık",
		StartDate: models.CivilDate(time.Now()), PeriodDays: 30}, nil)

	token := firstResult(t, performRequest(f.router, "POST", "/chat", `{"text":"bütçemi sil"}`)).Token
	w := performRequest(f.router, "POST", "/actions/confirm", `{"token":"`+token+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("beklenen 200, gelen %d (body: %s)", w.Code, w.Body.String())
	}
	if len(f.budgets.budgets) != 0 {
		t.Fatalf("onaydan sonra bütçe silinmiş olmalı")
	}
}

// TOCTOU: token bütçe id=1 için üretildi; bu arada bütçe silinip YENİSİ (id=2)
// kuruldu. Onay YENİ bütçeyi silmemeli — token bayat.
func TestConfirm_BudgetDelete_StaleTokenRejected(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{Intent: models.IntentBudgetDelete})
	f.budgets.seed(&models.Budget{ID: 1, UserID: chatUserID, Name: "Eski",
		StartDate: models.CivilDate(time.Now()), PeriodDays: 30}, nil)

	token := firstResult(t, performRequest(f.router, "POST", "/chat", `{"text":"bütçemi sil"}`)).Token

	// Kullanıcı bu arada bütçesini değiştirdi: eski gitti, yeni (id=2) geldi.
	_ = f.budgets.Delete(1)
	f.budgets.seed(&models.Budget{ID: 2, UserID: chatUserID, Name: "Yeni",
		StartDate: models.CivilDate(time.Now()), PeriodDays: 15}, nil)

	w := performRequest(f.router, "POST", "/actions/confirm", `{"token":"`+token+`"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("bayat token 404 vermeli, gelen %d (body: %s)", w.Code, w.Body.String())
	}
	// Yeni bütçe DOKUNULMAMIŞ olmalı.
	if _, ok := f.budgets.budgets[2]; !ok {
		t.Fatalf("bayat token yeni bütçeyi sildi — TOCTOU koruması başarısız")
	}
}

// budget_update: yıkıcı niyet — onay ister, hemen değiştirmez.
func TestChat_BudgetUpdate_RequiresConfirmation(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{
		Intent: models.IntentBudgetUpdate,
		Params: models.ActionParams{
			BudgetCategories: []models.BudgetCategoryParam{{CategoryRef: "Yeme", Amount: 2000}},
		},
	})
	f.budgets.seed(&models.Budget{ID: 1, UserID: chatUserID, Name: "Aylık",
		StartDate: models.CivilDate(time.Now()), PeriodDays: 30},
		[]models.BudgetCategory{{ID: 1, BudgetID: 1, CategoryID: 1, LimitAmount: 500}})

	res := firstResult(t, performRequest(f.router, "POST", "/chat", `{"text":"yeme limitini 2000 yap"}`))
	if res.Risk != models.RiskDestructive || !res.RequiresConfirmation || res.Token == "" {
		t.Fatalf("yıkıcı + onay + token beklendi, gelen: %+v", res)
	}
	// Henüz değişmemeli.
	if f.budgets.lines[1][0].LimitAmount != 500 {
		t.Fatalf("onaydan önce limit değişmemeli")
	}
}

// Onay akışı: mevcut limiti değiştirir, DİĞER kategorileri korur.
func TestConfirm_BudgetUpdate_ChangesLimitKeepsOthers(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{
		Intent: models.IntentBudgetUpdate,
		Params: models.ActionParams{
			BudgetCategories: []models.BudgetCategoryParam{{CategoryRef: "Yeme", Amount: 2000}},
		},
	})
	// İki kategorili bütçe: Yeme(1)=500, Bos Kategori(2)=300.
	f.budgets.seed(&models.Budget{ID: 1, UserID: chatUserID, Name: "Aylık",
		StartDate: models.CivilDate(time.Now()), PeriodDays: 30},
		[]models.BudgetCategory{
			{ID: 1, BudgetID: 1, CategoryID: 1, LimitAmount: 500},
			{ID: 2, BudgetID: 1, CategoryID: 2, LimitAmount: 300},
		})

	token := firstResult(t, performRequest(f.router, "POST", "/chat", `{"text":"yeme 2000"}`)).Token
	w := performRequest(f.router, "POST", "/actions/confirm", `{"token":"`+token+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("beklenen 200, gelen %d (body: %s)", w.Code, w.Body.String())
	}

	// Yeme 2000 oldu, Bos Kategori 300 KORUNDU.
	limits := map[int]float64{}
	for _, ln := range f.budgets.lines[1] {
		limits[ln.CategoryID] = ln.LimitAmount
	}
	if limits[1] != 2000 {
		t.Fatalf("Yeme limiti 2000 olmalı, gelen %v", limits[1])
	}
	if limits[2] != 300 {
		t.Fatalf("Bos Kategori limiti 300 korunmalı, gelen %v", limits[2])
	}
}

// Onay akışı: yeni kategori ekler, mevcudu korur.
func TestConfirm_BudgetUpdate_AddsCategory(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{
		Intent: models.IntentBudgetUpdate,
		Params: models.ActionParams{
			BudgetCategories: []models.BudgetCategoryParam{{CategoryRef: "Bos Kategori", Amount: 400}},
		},
	})
	f.budgets.seed(&models.Budget{ID: 1, UserID: chatUserID, Name: "Aylık",
		StartDate: models.CivilDate(time.Now()), PeriodDays: 30},
		[]models.BudgetCategory{{ID: 1, BudgetID: 1, CategoryID: 1, LimitAmount: 500}})

	token := firstResult(t, performRequest(f.router, "POST", "/chat", `{"text":"bos kategoriye 400 ekle"}`)).Token
	w := performRequest(f.router, "POST", "/actions/confirm", `{"token":"`+token+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("beklenen 200, gelen %d (body: %s)", w.Code, w.Body.String())
	}
	if len(f.budgets.lines[1]) != 2 {
		t.Fatalf("2 kategori olmalı (mevcut + yeni), gelen %d", len(f.budgets.lines[1]))
	}
}

// Değişecek bir şey yoksa reddet (boş liste, dönem 0, isim yok).
func TestChat_BudgetUpdate_NothingToChange(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{Intent: models.IntentBudgetUpdate})
	f.budgets.seed(&models.Budget{ID: 1, UserID: chatUserID, Name: "Aylık",
		StartDate: models.CivilDate(time.Now()), PeriodDays: 30}, nil)

	res := firstResult(t, performRequest(f.router, "POST", "/chat", `{"text":"bütçeyi güncelle"}`))
	if res.RequiresConfirmation {
		t.Fatalf("değişiklik yokken token üretilmemeli")
	}
	if res.Error != "nothing to change was specified" {
		t.Fatalf("anlaşılır mesaj beklendi, gelen: %q", res.Error)
	}
}

// Bütçesi olmayan kullanıcı değiştirmek isterse.
func TestChat_BudgetUpdate_NoBudget(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{
		Intent: models.IntentBudgetUpdate,
		Params: models.ActionParams{
			BudgetCategories: []models.BudgetCategoryParam{{CategoryRef: "Yeme", Amount: 2000}},
		},
	})
	res := firstResult(t, performRequest(f.router, "POST", "/chat", `{"text":"yeme 2000"}`))
	if res.RequiresConfirmation {
		t.Fatalf("bütçe yokken token üretilmemeli")
	}
	if res.Error != "you don't have a budget to modify" {
		t.Fatalf("anlaşılır mesaj beklendi, gelen: %q", res.Error)
	}
}

// TOCTOU: token id=1 içindi; bütçe silinip yenisi (id=2) kuruldu -> reddet.
func TestConfirm_BudgetUpdate_StaleTokenRejected(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{
		Intent: models.IntentBudgetUpdate,
		Params: models.ActionParams{
			BudgetCategories: []models.BudgetCategoryParam{{CategoryRef: "Yeme", Amount: 2000}},
		},
	})
	f.budgets.seed(&models.Budget{ID: 1, UserID: chatUserID, Name: "Eski",
		StartDate: models.CivilDate(time.Now()), PeriodDays: 30},
		[]models.BudgetCategory{{ID: 1, BudgetID: 1, CategoryID: 1, LimitAmount: 500}})

	token := firstResult(t, performRequest(f.router, "POST", "/chat", `{"text":"yeme 2000"}`)).Token

	_ = f.budgets.Delete(1)
	f.budgets.seed(&models.Budget{ID: 2, UserID: chatUserID, Name: "Yeni",
		StartDate: models.CivilDate(time.Now()), PeriodDays: 15},
		[]models.BudgetCategory{{ID: 1, BudgetID: 2, CategoryID: 1, LimitAmount: 999}})

	w := performRequest(f.router, "POST", "/actions/confirm", `{"token":"`+token+`"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("bayat token 404 vermeli, gelen %d", w.Code)
	}
	// Yeni bütçe DEĞİŞMEMİŞ olmalı.
	if f.budgets.lines[2][0].LimitAmount != 999 {
		t.Fatalf("bayat token yeni bütçeyi değiştirdi — TOCTOU koruması başarısız")
	}
}

// budget_view göreli dönem: period_offset ile geçmiş dönem.
func TestChat_BudgetView_PreviousPeriod(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{
		Intent: models.IntentBudgetView,
		Params: models.ActionParams{PeriodOffset: -1},
	})
	f.budgets.seed(&models.Budget{ID: 1, UserID: chatUserID, Name: "Aylık",
		StartDate: models.CivilDate(time.Now().AddDate(0, 0, -5)), PeriodDays: 30},
		[]models.BudgetCategory{{ID: 1, BudgetID: 1, CategoryID: 1, LimitAmount: 500}})

	res := firstResult(t, performRequest(f.router, "POST", "/chat", `{"text":"geçen dönem bütçem"}`))
	if res.Error != "" {
		t.Fatalf("beklenmeyen hata: %s", res.Error)
	}
	raw, _ := json.Marshal(res.Data)
	var view models.BudgetView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("BudgetView çözülemedi: %v", err)
	}
	if view.Period.Offset != -1 {
		t.Fatalf("offset -1 beklendi, gelen %d", view.Period.Offset)
	}
	if !view.Period.Historical {
		t.Fatalf("geçmiş dönem historical:true olmalı")
	}
}

// Aşırı offset: Duration taşmasını engelle, anlaşılır mesaj ver.
func TestChat_BudgetView_OffsetOutOfRange(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{
		Intent: models.IntentBudgetView,
		Params: models.ActionParams{PeriodOffset: 99999},
	})
	f.budgets.seed(&models.Budget{ID: 1, UserID: chatUserID, Name: "Aylık",
		StartDate: models.CivilDate(time.Now()), PeriodDays: 30}, nil)

	res := firstResult(t, performRequest(f.router, "POST", "/chat", `{"text":"999 dönem önceki bütçem"}`))
	if res.Error != "period range is too large" {
		t.Fatalf("anlaşılır sınır mesajı beklendi, gelen: %q", res.Error)
	}
}
