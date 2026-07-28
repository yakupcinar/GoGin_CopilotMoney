package handlers

import (
	"GoGinMoneyCopilot/chat"
	"GoGinMoneyCopilot/models"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// Tests for the chat flow.
//
// SCOPE: handler + chat.ActionService are tested TOGETHER. Only the AI is
// fake — the repositories are fake too, but the real service logic runs.
// So the whitelist, ownership checks, validation layer, and confirmation
// flow all genuinely execute.
//
// Since we fully control the model's output, we can deterministically test
// "what happens if the model talks nonsense" scenarios.

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

// firstResult — resolves the first action in the response.
func firstResult(t *testing.T, w *httptest.ResponseRecorder) chat.Result {
	t.Helper()
	var body struct {
		Results []chat.Result `json:"results"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response could not be parsed: %v (%s)", err, w.Body.String())
	}
	if len(body.Results) == 0 {
		t.Fatalf("no results: %s", w.Body.String())
	}
	return body.Results[0]
}

func txAction(params models.ActionParams) models.ParsedAction {
	return models.ParsedAction{
		Intent: models.IntentCreateTransaction, Params: params, Confidence: 0.9,
	}
}

// ---------------------------------------------------------------------------
// Input boundaries — BEFORE reaching the AI
// ---------------------------------------------------------------------------

func TestChat_EmptyText_Returns400(t *testing.T) {
	f := newChatFixture(t)

	w := performRequest(f.router, "POST", "/chat", `{"text":""}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if f.parser.calls != 0 {
		t.Fatal("AI was called for empty text — wasting tokens for nothing")
	}
}

func TestChat_TooLongText_RejectedBeforeAICall(t *testing.T) {
	f := newChatFixture(t)

	w := performRequest(f.router, "POST", "/chat",
		`{"text":"`+strings.Repeat("a", 600)+`"}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if f.parser.calls != 0 {
		t.Fatal("AI was called for overly long text")
	}
}

// If the AI service goes down it's 503, NOT 500: this is not our fault, it's the external dependency's.
func TestChat_ParserFailure_Returns503(t *testing.T) {
	f := newChatFixture(t)
	f.parser.err = errors.New("groq erişilemiyor")

	w := performRequest(f.router, "POST", "/chat", `{"text":"kahve 50 tl"}`)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d (%s)", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "groq") {
		t.Fatal("internal error detail leaked to the client")
	}
}

// If GROQ_API_KEY is missing the service is nil — not 404, an explanatory 503.
func TestChat_ServiceNotConfigured_Returns503(t *testing.T) {
	h := NewChatHandler(nil)
	r := gin.New()
	r.Use(authAs(chatUserID, models.RoleClient))
	r.POST("/chat", h.Chat)

	w := performRequest(r, "POST", "/chat", `{"text":"kahve"}`)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Whitelist — an intent the model makes up NEVER works
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
		t.Fatalf("expected 2 results, got %d", len(body.Results))
	}
	for i, r := range body.Results {
		if r.Error == "" {
			t.Fatalf("action %d was not rejected: %+v", i+1, r)
		}
		if r.Payload != nil || r.Token != "" {
			t.Fatalf("payload/token was produced for an unauthorized intent: %+v", r)
		}
	}
}

// ---------------------------------------------------------------------------
// Transaction creation — validation layer
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
		t.Fatalf("unexpected error: %s", res.Error)
	}
	payload, _ := json.Marshal(res.Payload)
	var input models.CreateTransactionInput
	json.Unmarshal(payload, &input)

	// account_id does NOT come from the model — it comes from the request/default.
	if input.AccountID != chatAccountID {
		t.Fatalf("account_id is wrong: %d", input.AccountID)
	}
	if input.Amount != 50.5 {
		t.Fatalf("amount is wrong: %v", input.Amount)
	}
}

// If the model can't find an amount it writes 0 (field is required). This cannot be fixed -> reject.
func TestChat_ZeroAmount_Rejected(t *testing.T) {
	catID := 1
	f := newChatFixture(t, txAction(models.ActionParams{
		Amount: 0, Type: "expense", Description: "market",
		CategoryID: &catID, TransactionDate: time.Now().Format("2006-01-02"),
	}))

	w := performRequest(f.router, "POST", "/chat", `{"text":"bugün markete gittim"}`)

	res := firstResult(t, w)
	if res.Error == "" {
		t.Fatal("a draft was produced while amount was 0")
	}
	if res.Payload != nil {
		t.Fatal("payload was produced with an invalid amount")
	}
}

// If the model suggests a category NOT in the list: drop + warn (don't reject).
func TestChat_UnknownCategory_DroppedWithWarning(t *testing.T) {
	bogus := 999
	f := newChatFixture(t, txAction(models.ActionParams{
		Amount: 50, Type: "expense", Description: "kahve",
		CategoryID: &bogus, TransactionDate: time.Now().Format("2006-01-02"),
	}))

	w := performRequest(f.router, "POST", "/chat", `{"text":"kahve 50"}`)

	res := firstResult(t, w)
	if res.Payload != nil {
		t.Fatal("payload was produced with a made-up category")
	}
	if len(res.NeedsInput) == 0 {
		t.Fatal("category_id was not marked as missing")
	}
	if len(res.Warnings) == 0 {
		t.Fatal("no warning was given to the user")
	}
}

// ACTUAL OBSERVED BUG: the model resolved "last Tuesday" as 2024.
// The date window must catch this and clamp it to today.
func TestChat_StaleYear_ClampedToToday(t *testing.T) {
	catID := 1
	f := newChatFixture(t, txAction(models.ActionParams{
		Amount: 145, Type: "expense", Description: "taksi",
		CategoryID: &catID, TransactionDate: "2020-07-16", // too far in the past
	}))

	w := performRequest(f.router, "POST", "/chat", `{"text":"geçen salı taksi 145"}`)

	res := firstResult(t, w)
	payload, _ := json.Marshal(res.Payload)
	var input models.CreateTransactionInput
	json.Unmarshal(payload, &input)

	if input.TransactionDate.Year() == 2020 {
		t.Fatal("the stale year got through — date window is not working")
	}
	if len(res.Warnings) == 0 {
		t.Fatal("date was corrected but the user was not informed")
	}
}

// ---------------------------------------------------------------------------
// Ownership — another user's record cannot be accessed
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
		t.Fatal("access to another user's account was not rejected")
	}
	if res.Data != nil {
		t.Fatal("another user's account data leaked")
	}
}

// Global categories (UserID == nil) cannot be modified via chat.
func TestChat_GlobalCategoryDelete_Rejected(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{
		Intent: models.IntentDeleteCategory,
		Params: models.ActionParams{TargetRef: "Global"},
	})

	w := performRequest(f.router, "POST", "/chat", `{"text":"global kategorisini sil"}`)

	res := firstResult(t, w)
	if res.Error == "" {
		t.Fatal("global category deletion was not rejected")
	}
	if res.Token != "" {
		t.Fatal("a confirmation code was produced for a global category")
	}
}

// ---------------------------------------------------------------------------
// Destructive operations — token flow
// ---------------------------------------------------------------------------

func TestChat_DeleteCategory_IssuesConfirmationToken(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{
		Intent: models.IntentDeleteCategory,
		Params: models.ActionParams{TargetRef: "Bos Kategori"},
	})

	w := performRequest(f.router, "POST", "/chat", `{"text":"bos kategori sil"}`)

	res := firstResult(t, w)
	if !res.RequiresConfirmation || res.Token == "" {
		t.Fatalf("no confirmation code was produced: %+v", res)
	}
	if res.Summary == "" {
		t.Fatal("no summary — what will the frontend show in the popup?")
	}
	if !strings.Contains(res.Summary, "Bos Kategori") {
		t.Fatalf("summary does not mention the target: %q", res.Summary)
	}
}

// Confirmation must NOT be asked for something that cannot be deleted.
// The user shouldn't press "Yes" and then get an error.
func TestChat_DeleteCategoryInUse_NoTokenIssued(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{
		Intent: models.IntentDeleteCategory,
		Params: models.ActionParams{TargetRef: "Yeme"}, // used by transaction #100
	})

	w := performRequest(f.router, "POST", "/chat", `{"text":"yeme kategorisini sil"}`)

	res := firstResult(t, w)
	if res.Token != "" {
		t.Fatal("a confirmation code was produced for a category in use")
	}
	if res.Error == "" {
		t.Fatal("the reason was not reported to the user")
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
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if _, err := f.categories.GetByID(context.Background(), 2); err == nil {
		t.Fatal("category was not deleted")
	}
}

// The token is SINGLE-USE.
func TestConfirm_ReusedToken_Rejected(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{
		Intent: models.IntentDeleteCategory,
		Params: models.ActionParams{TargetRef: "Bos Kategori"},
	})
	token := firstResult(t, performRequest(f.router, "POST", "/chat", `{"text":"sil"}`)).Token

	performRequest(f.router, "POST", "/actions/confirm", `{"token":"`+token+`"}`)
	second := performRequest(f.router, "POST", "/actions/confirm", `{"token":"`+token+`"}`)

	if second.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on second use, got %d", second.Code)
	}
}

func TestConfirm_UnknownToken_Rejected(t *testing.T) {
	f := newChatFixture(t)

	w := performRequest(f.router, "POST", "/actions/confirm", `{"token":"act_uydurma"}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// Another user's token cannot be used — and the reason must be
// INDISTINGUISHABLE (otherwise the token's existence leaks).
func TestConfirm_ForeignToken_RejectedIndistinguishably(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{
		Intent: models.IntentDeleteCategory,
		Params: models.ActionParams{TargetRef: "Bos Kategori"},
	})
	token := firstResult(t, performRequest(f.router, "POST", "/chat", `{"text":"sil"}`)).Token

	// Same service, DIFFERENT user.
	svc := chat.NewActionService(f.parser, f.accounts, f.categories, f.txs, f.budgets, f.pending)
	other := gin.New()
	other.Use(authAs(otherUserID, models.RoleClient))
	other.POST("/actions/confirm", NewChatHandler(svc).Confirm)

	foreign := performRequest(other, "POST", "/actions/confirm", `{"token":"`+token+`"}`)
	unknown := performRequest(other, "POST", "/actions/confirm", `{"token":"act_hicyok"}`)

	if foreign.Code != http.StatusBadRequest {
		t.Fatalf("another user's token was accepted: %d", foreign.Code)
	}
	if foreign.Body.String() != unknown.Body.String() {
		t.Fatalf("responses differ -> token existence leaks:\n  foreign: %s\n  unknown: %s",
			foreign.Body.String(), unknown.Body.String())
	}
}

// TOCTOU: if the category becomes in-use AFTER the token was produced,
// it must be re-checked and blocked at confirmation time.
func TestConfirm_TargetBecameInUse_Blocked(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{
		Intent: models.IntentDeleteCategory,
		Params: models.ActionParams{TargetRef: "Bos Kategori"},
	})
	token := firstResult(t, performRequest(f.router, "POST", "/chat", `{"text":"sil"}`)).Token

	// The world changed: a transaction was added to the category.
	f.txs.seed(&models.Transaction{
		ID: 200, AccountID: chatAccountID, CategoryID: 2, Amount: 10,
		Type: "expense", Description: "yeni", TransactionDate: time.Now(),
	})

	w := performRequest(f.router, "POST", "/actions/confirm", `{"token":"`+token+`"}`)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d (%s)", w.Code, w.Body.String())
	}
	if _, err := f.categories.GetByID(context.Background(), 2); err != nil {
		t.Fatal("category was deleted — TOCTOU protection did not work")
	}
}

// budget_view: viewing a budget via chat (read intent, no confirmation).
//
// TRUST BOUNDARY: the fake parser returns IntentBudgetView (as if the model
// produced it); what's real is chat.ActionService routing this intent to
// BuildBudgetView and producing the SAME result as the HTTP handler.
func TestChat_BudgetView_ReturnsCurrentPeriod(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{Intent: models.IntentBudgetView})
	// A budget with a 500 limit for category 1 (Yeme), covering today.
	f.budgets.seed(
		&models.Budget{ID: 1, UserID: chatUserID, Name: "Aylık",
			StartDate: models.CivilDate(time.Now().AddDate(0, 0, -5)), PeriodDays: 30},
		[]models.BudgetCategory{{ID: 1, BudgetID: 1, CategoryID: 1, LimitAmount: 500}},
	)

	w := performRequest(f.router, "POST", "/chat", `{"text":"bütçemi göster"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}
	res := firstResult(t, w)
	if res.Error != "" {
		t.Fatalf("unexpected error: %s", res.Error)
	}
	if res.Risk != models.RiskRead {
		t.Fatalf("budget_view should be a read intent, got risk: %q", res.Risk)
	}

	// res.Data is a BudgetView serialized to JSON; decode it back and verify.
	raw, _ := json.Marshal(res.Data)
	var view models.BudgetView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("BudgetView could not be decoded: %v", err)
	}
	// The fixture's 50 TL "kahve" transaction (category 1) must count in this period.
	if view.TotalSpent != 50 {
		t.Fatalf("expected spending of 50, got %v", view.TotalSpent)
	}
	if view.TotalLimit != 500 {
		t.Fatalf("expected limit of 500, got %v", view.TotalLimit)
	}
}

// If a user without a budget asks for one via chat: 200 + a clear error,
// NOT 500 (it's a user error, not a server failure).
func TestChat_BudgetView_NoBudget(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{Intent: models.IntentBudgetView})

	w := performRequest(f.router, "POST", "/chat", `{"text":"bütçemi göster"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	res := firstResult(t, w)
	if res.Error != "you don't have a budget yet" {
		t.Fatalf("expected a clear 'no budget' message, got: %q", res.Error)
	}
}

// budget_set: setting up a budget via chat (create tier — does NOT write, produces a draft).
//
// Same pattern as create_transaction: the result is res.Payload (a CreateBudgetInput);
// the frontend sends it via POST /budgets. The real write goes through the REST gate.
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
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}
	res := firstResult(t, w)
	if res.Error != "" {
		t.Fatalf("unexpected error: %s", res.Error)
	}
	if res.Risk != models.RiskCreate {
		t.Fatalf("budget_set should be a create tier, got: %q", res.Risk)
	}

	raw, _ := json.Marshal(res.Payload)
	var input models.CreateBudgetInput
	if err := json.Unmarshal(raw, &input); err != nil {
		t.Fatalf("CreateBudgetInput could not be decoded: %v", err)
	}
	if input.PeriodDays != 30 {
		t.Fatalf("expected period_days 30, got %d", input.PeriodDays)
	}
	if len(input.Categories) != 1 || input.Categories[0].CategoryID != 1 || input.Categories[0].LimitAmount != 500 {
		t.Fatalf("category row was decoded incorrectly: %+v", input.Categories)
	}
	if input.StartDate != time.Now().Format(models.DateLayout) {
		t.Fatalf("start date should be today, got %q", input.StartDate)
	}
}

// If the period is not given: don't MAKE UP a value, ask the user.
func TestChat_BudgetSet_MissingPeriodNeedsInput(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{
		Intent: models.IntentBudgetSet,
		Params: models.ActionParams{
			BudgetCategories: []models.BudgetCategoryParam{{CategoryRef: "Yeme", Amount: 500}},
		},
	})
	res := firstResult(t, performRequest(f.router, "POST", "/chat", `{"text":"yemeye 500 bütçe"}`))
	if res.Payload != nil {
		t.Fatalf("a draft must not be produced with a missing period")
	}
	found := false
	for _, n := range res.NeedsInput {
		if n == "period_days" {
			found = true
		}
	}
	if !found {
		t.Fatalf("period_days should be in NeedsInput, got: %v", res.NeedsInput)
	}
}

// Unknown category: reject (the model can't make up an id, ref could not be resolved).
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
		t.Fatalf("unknown category should have been rejected")
	}
	if res.Payload != nil {
		t.Fatalf("a draft must not be produced when there is an error")
	}
}

// An income category cannot be budgeted.
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
		t.Fatalf("income category should have been rejected (error: %q)", res.Error)
	}
}

// Reject if the same category is given twice.
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
		t.Fatalf("duplicate category should have been rejected")
	}
}

// A user who already has a budget: create would conflict, give a clear message.
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
		t.Fatalf("a draft must not be produced while a budget exists")
	}
	if !strings.Contains(res.Error, "you already have a budget") {
		t.Fatalf("expected a clear 'already exists' message, got: %q", res.Error)
	}
}

// budget_delete: destructive intent — REQUIRES CONFIRMATION, does not delete immediately.
func TestChat_BudgetDelete_RequiresConfirmation(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{Intent: models.IntentBudgetDelete})
	f.budgets.seed(&models.Budget{ID: 1, UserID: chatUserID, Name: "Aylık",
		StartDate: models.CivilDate(time.Now()), PeriodDays: 30},
		[]models.BudgetCategory{{ID: 1, BudgetID: 1, CategoryID: 1, LimitAmount: 500}})

	res := firstResult(t, performRequest(f.router, "POST", "/chat", `{"text":"bütçemi sil"}`))
	if res.Risk != models.RiskDestructive {
		t.Fatalf("expected a destructive tier, got: %q", res.Risk)
	}
	if !res.RequiresConfirmation || res.Token == "" {
		t.Fatalf("expected confirmation + token, got: %+v", res)
	}
	// Must NOT be deleted yet — only awaiting confirmation.
	if len(f.budgets.budgets) != 1 {
		t.Fatalf("budget must not be deleted before confirmation")
	}
}

// A user without a budget: NO token is produced at all (don't bother asking "are you sure?").
func TestChat_BudgetDelete_NoBudget(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{Intent: models.IntentBudgetDelete})
	res := firstResult(t, performRequest(f.router, "POST", "/chat", `{"text":"bütçemi sil"}`))
	if res.RequiresConfirmation || res.Token != "" {
		t.Fatalf("a token must not be produced when there is no budget")
	}
	if res.Error != "you don't have a budget to delete" {
		t.Fatalf("expected a clear message, got: %q", res.Error)
	}
}

// The full confirmation flow: chat -> token -> confirm -> actually deleted.
func TestConfirm_BudgetDelete_Deletes(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{Intent: models.IntentBudgetDelete})
	f.budgets.seed(&models.Budget{ID: 1, UserID: chatUserID, Name: "Aylık",
		StartDate: models.CivilDate(time.Now()), PeriodDays: 30}, nil)

	token := firstResult(t, performRequest(f.router, "POST", "/chat", `{"text":"bütçemi sil"}`)).Token
	w := performRequest(f.router, "POST", "/actions/confirm", `{"token":"`+token+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}
	if len(f.budgets.budgets) != 0 {
		t.Fatalf("budget should be deleted after confirmation")
	}
}

// TOCTOU: the token was produced for budget id=1; meanwhile it was deleted
// and a NEW one (id=2) was created. Confirmation must not delete the NEW
// budget — the token is stale.
func TestConfirm_BudgetDelete_StaleTokenRejected(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{Intent: models.IntentBudgetDelete})
	f.budgets.seed(&models.Budget{ID: 1, UserID: chatUserID, Name: "Eski",
		StartDate: models.CivilDate(time.Now()), PeriodDays: 30}, nil)

	token := firstResult(t, performRequest(f.router, "POST", "/chat", `{"text":"bütçemi sil"}`)).Token

	// The user changed their budget in the meantime: old one gone, new one (id=2) arrived.
	_ = f.budgets.Delete(context.Background(), 1)
	f.budgets.seed(&models.Budget{ID: 2, UserID: chatUserID, Name: "Yeni",
		StartDate: models.CivilDate(time.Now()), PeriodDays: 15}, nil)

	w := performRequest(f.router, "POST", "/actions/confirm", `{"token":"`+token+`"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("stale token should return 404, got %d (body: %s)", w.Code, w.Body.String())
	}
	// The new budget must be UNTOUCHED.
	if _, ok := f.budgets.budgets[2]; !ok {
		t.Fatalf("stale token deleted the new budget — TOCTOU protection failed")
	}
}

// budget_update: destructive intent — requires confirmation, does not change immediately.
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
		t.Fatalf("expected destructive + confirmation + token, got: %+v", res)
	}
	// Must not have changed yet.
	if f.budgets.lines[1][0].LimitAmount != 500 {
		t.Fatalf("limit must not change before confirmation")
	}
}

// Confirmation flow: changes the existing limit, PRESERVES the other categories.
func TestConfirm_BudgetUpdate_ChangesLimitKeepsOthers(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{
		Intent: models.IntentBudgetUpdate,
		Params: models.ActionParams{
			BudgetCategories: []models.BudgetCategoryParam{{CategoryRef: "Yeme", Amount: 2000}},
		},
	})
	// A budget with two categories: Yeme(1)=500, Bos Kategori(2)=300.
	f.budgets.seed(&models.Budget{ID: 1, UserID: chatUserID, Name: "Aylık",
		StartDate: models.CivilDate(time.Now()), PeriodDays: 30},
		[]models.BudgetCategory{
			{ID: 1, BudgetID: 1, CategoryID: 1, LimitAmount: 500},
			{ID: 2, BudgetID: 1, CategoryID: 2, LimitAmount: 300},
		})

	token := firstResult(t, performRequest(f.router, "POST", "/chat", `{"text":"yeme 2000"}`)).Token
	w := performRequest(f.router, "POST", "/actions/confirm", `{"token":"`+token+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	// Yeme became 2000, Bos Kategori 300 was PRESERVED.
	limits := map[int]float64{}
	for _, ln := range f.budgets.lines[1] {
		limits[ln.CategoryID] = ln.LimitAmount
	}
	if limits[1] != 2000 {
		t.Fatalf("Yeme limit should be 2000, got %v", limits[1])
	}
	if limits[2] != 300 {
		t.Fatalf("Bos Kategori limit should be preserved at 300, got %v", limits[2])
	}
}

// Confirmation flow: adds a new category, preserves the existing one.
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
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}
	if len(f.budgets.lines[1]) != 2 {
		t.Fatalf("should have 2 categories (existing + new), got %d", len(f.budgets.lines[1]))
	}
}

// Reject if there is nothing to change (empty list, period 0, no name).
func TestChat_BudgetUpdate_NothingToChange(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{Intent: models.IntentBudgetUpdate})
	f.budgets.seed(&models.Budget{ID: 1, UserID: chatUserID, Name: "Aylık",
		StartDate: models.CivilDate(time.Now()), PeriodDays: 30}, nil)

	res := firstResult(t, performRequest(f.router, "POST", "/chat", `{"text":"bütçeyi güncelle"}`))
	if res.RequiresConfirmation {
		t.Fatalf("a token must not be produced when there is no change")
	}
	if res.Error != "nothing to change was specified" {
		t.Fatalf("expected a clear message, got: %q", res.Error)
	}
}

// If a user without a budget tries to modify it.
func TestChat_BudgetUpdate_NoBudget(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{
		Intent: models.IntentBudgetUpdate,
		Params: models.ActionParams{
			BudgetCategories: []models.BudgetCategoryParam{{CategoryRef: "Yeme", Amount: 2000}},
		},
	})
	res := firstResult(t, performRequest(f.router, "POST", "/chat", `{"text":"yeme 2000"}`))
	if res.RequiresConfirmation {
		t.Fatalf("a token must not be produced when there is no budget")
	}
	if res.Error != "you don't have a budget to modify" {
		t.Fatalf("expected a clear message, got: %q", res.Error)
	}
}

// TOCTOU: token was for id=1; budget was deleted and a new one (id=2) was created -> reject.
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

	_ = f.budgets.Delete(context.Background(), 1)
	f.budgets.seed(&models.Budget{ID: 2, UserID: chatUserID, Name: "Yeni",
		StartDate: models.CivilDate(time.Now()), PeriodDays: 15},
		[]models.BudgetCategory{{ID: 1, BudgetID: 2, CategoryID: 1, LimitAmount: 999}})

	w := performRequest(f.router, "POST", "/actions/confirm", `{"token":"`+token+`"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("stale token should return 404, got %d", w.Code)
	}
	// The new budget must be UNCHANGED.
	if f.budgets.lines[2][0].LimitAmount != 999 {
		t.Fatalf("stale token modified the new budget — TOCTOU protection failed")
	}
}

// budget_view relative period: a past period via period_offset.
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
		t.Fatalf("unexpected error: %s", res.Error)
	}
	raw, _ := json.Marshal(res.Data)
	var view models.BudgetView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("BudgetView could not be decoded: %v", err)
	}
	if view.Period.Offset != -1 {
		t.Fatalf("expected offset -1, got %d", view.Period.Offset)
	}
	if !view.Period.Historical {
		t.Fatalf("a past period should have historical:true")
	}
}

// Excessive offset: prevent Duration overflow, give a clear message.
func TestChat_BudgetView_OffsetOutOfRange(t *testing.T) {
	f := newChatFixture(t, models.ParsedAction{
		Intent: models.IntentBudgetView,
		Params: models.ActionParams{PeriodOffset: 99999},
	})
	f.budgets.seed(&models.Budget{ID: 1, UserID: chatUserID, Name: "Aylık",
		StartDate: models.CivilDate(time.Now()), PeriodDays: 30}, nil)

	res := firstResult(t, performRequest(f.router, "POST", "/chat", `{"text":"999 dönem önceki bütçem"}`))
	if res.Error != "period range is too large" {
		t.Fatalf("expected a clear range-limit message, got: %q", res.Error)
	}
}
