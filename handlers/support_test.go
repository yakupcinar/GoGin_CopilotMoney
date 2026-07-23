package handlers

// Bu dosya testlerde kullanılan "sahte" (fake) repository'leri ve yardımcı
// fonksiyonları içerir. Gerçek veritabanı yerine bellekteki map'ler kullanılır;
// böylece testler Postgres'e ihtiyaç duymaz, milisaniyeler içinde çalışır.
//
// Her fake, gerçek repository interface'ini karşılar. Aşağıdaki derleme-zamanı
// kontrolleri bunu garanti eder: eğer bir fake interface'i tam karşılamıyorsa
// proje derlenmez.
//
// Fake'ler ayrıca hata enjeksiyonu destekler: repo.failOn("GetByID", errBoom)
// dedikten sonra o metod her çağrıldığında verilen hatayı döner. Böylece
// "veritabanı patlarsa handler ne yapıyor?" (500 yolları) test edilebilir.

import (
	"GoGinMoneyCopilot/ai"
	"GoGinMoneyCopilot/models"
	"GoGinMoneyCopilot/repositories"
	"bytes"
	"context"
	"errors"
	"net/http/httptest"
	"time"

	"github.com/gin-gonic/gin"
)

var (
	_ repositories.AccountRepository       = (*fakeAccountRepo)(nil)
	_ repositories.CategoryRepository      = (*fakeCategoryRepo)(nil)
	_ repositories.TransactionRepository   = (*fakeTransactionRepo)(nil)
	_ repositories.UserRepository          = (*fakeUserRepo)(nil)
	_ repositories.TokenRepository         = (*fakeTokenRepo)(nil)
	_ repositories.RefreshTokenRepository  = (*fakeRefreshRepo)(nil)
	_ repositories.PendingActionRepository = (*fakePendingRepo)(nil)
	_ repositories.BudgetRepository        = (*fakeBudgetRepo)(nil)
	_ ai.ActionParser                      = (*fakeActionParser)(nil)
)

// errBoom, testlerde "beklenmeyen altyapı hatası"nı temsil eder.
// Handler'ın bunu bilinen domain hatalarından (ErrAccountNotFound gibi)
// ayırıp 500 döndürmesi beklenir.
var errBoom = errors.New("veritabanı bağlantısı koptu")

// ---- hata enjeksiyonu ----

// errInjector, fake repo'lara gömülerek (embed) onlara failOn/injected
// yeteneğini kazandırır. Go'da struct gömme (composition) böyle çalışır:
// gömülen tipin metodları, gömen tipin metodlarıymış gibi çağrılabilir.
type errInjector struct {
	errs map[string]error
}

func newErrInjector() errInjector {
	return errInjector{errs: map[string]error{}}
}

// failOn: verilen metod adı çağrıldığında err dönsün.
func (e *errInjector) failOn(method string, err error) {
	e.errs[method] = err
}

// injected: bu metod için enjekte edilmiş hata varsa döner, yoksa nil.
func (e *errInjector) injected(method string) error {
	return e.errs[method]
}

// ---- fakeAccountRepo ----

type fakeAccountRepo struct {
	errInjector
	accounts map[int]*models.Account
	// inUse: silinince ErrAccountInUse dönmesi için işaretli id'ler
	// (gerçek repo'da bunu foreign key kısıtı yapar)
	inUse  map[int]bool
	nextID int
}

func newFakeAccountRepo() *fakeAccountRepo {
	return &fakeAccountRepo{
		errInjector: newErrInjector(),
		accounts:    map[int]*models.Account{},
		inUse:       map[int]bool{},
		nextID:      1,
	}
}

func (r *fakeAccountRepo) seed(acc *models.Account) {
	r.accounts[acc.ID] = acc
	if acc.ID >= r.nextID {
		r.nextID = acc.ID + 1
	}
}

func (r *fakeAccountRepo) Create(name string, userID int) error {
	if err := r.injected("Create"); err != nil {
		return err
	}
	acc := &models.Account{ID: r.nextID, Name: name, UserID: userID, CreatedAt: time.Now()}
	r.accounts[r.nextID] = acc
	r.nextID++
	return nil
}

func (r *fakeAccountRepo) GetByID(accountID int) (*models.Account, error) {
	if err := r.injected("GetByID"); err != nil {
		return nil, err
	}
	acc, ok := r.accounts[accountID]
	if !ok {
		return nil, repositories.ErrAccountNotFound
	}
	return acc, nil
}

func (r *fakeAccountRepo) GetByIDForUser(accountID, userID int) (*models.Account, error) {
	if err := r.injected("GetByIDForUser"); err != nil {
		return nil, err
	}
	acc, ok := r.accounts[accountID]
	if !ok || acc.UserID != userID {
		return nil, repositories.ErrAccountNotFound
	}
	return acc, nil
}

func (r *fakeAccountRepo) ListForUser(userID int) ([]models.Account, error) {
	if err := r.injected("ListForUser"); err != nil {
		return nil, err
	}
	var out []models.Account
	for _, acc := range r.accounts {
		if acc.UserID == userID {
			out = append(out, *acc)
		}
	}
	return out, nil
}

func (r *fakeAccountRepo) Update(accountID int, name string) error {
	if err := r.injected("Update"); err != nil {
		return err
	}
	acc, ok := r.accounts[accountID]
	if !ok {
		return repositories.ErrAccountNotFound
	}
	acc.Name = name
	return nil
}

func (r *fakeAccountRepo) Delete(accountID int) error {
	if err := r.injected("Delete"); err != nil {
		return err
	}
	if _, ok := r.accounts[accountID]; !ok {
		return repositories.ErrAccountNotFound
	}
	if r.inUse[accountID] {
		return repositories.ErrAccountInUse
	}
	delete(r.accounts, accountID)
	return nil
}

// ---- fakeCategoryRepo ----

type fakeCategoryRepo struct {
	errInjector
	categories map[int]*models.Category
	inUse      map[int]bool // silinince ErrCategoryInUse dönmesi için işaretli id'ler
	nextID     int
}

func newFakeCategoryRepo() *fakeCategoryRepo {
	return &fakeCategoryRepo{
		errInjector: newErrInjector(),
		categories:  map[int]*models.Category{},
		inUse:       map[int]bool{},
		nextID:      1,
	}
}

func (r *fakeCategoryRepo) seed(cat *models.Category) {
	r.categories[cat.ID] = cat
	if cat.ID >= r.nextID {
		r.nextID = cat.ID + 1
	}
}

func (r *fakeCategoryRepo) Create(name, categoryType string, userID *int) error {
	if err := r.injected("Create"); err != nil {
		return err
	}
	cat := &models.Category{ID: r.nextID, Name: name, Type: categoryType, UserID: userID}
	r.categories[r.nextID] = cat
	r.nextID++
	return nil
}

func (r *fakeCategoryRepo) GetForUser(userID int) ([]models.Category, error) {
	if err := r.injected("GetForUser"); err != nil {
		return nil, err
	}
	var out []models.Category
	for _, cat := range r.categories {
		if cat.UserID == nil || *cat.UserID == userID {
			out = append(out, *cat)
		}
	}
	return out, nil
}

func (r *fakeCategoryRepo) GetByID(categoryID int) (*models.Category, error) {
	if err := r.injected("GetByID"); err != nil {
		return nil, err
	}
	cat, ok := r.categories[categoryID]
	if !ok {
		return nil, repositories.ErrCategoryNotFound
	}
	return cat, nil
}

func (r *fakeCategoryRepo) Update(categoryID int, name, categoryType string) error {
	if err := r.injected("Update"); err != nil {
		return err
	}
	cat, ok := r.categories[categoryID]
	if !ok {
		return repositories.ErrCategoryNotFound
	}
	cat.Name = name
	cat.Type = categoryType
	return nil
}

func (r *fakeCategoryRepo) Delete(categoryID int) error {
	if err := r.injected("Delete"); err != nil {
		return err
	}
	if _, ok := r.categories[categoryID]; !ok {
		return repositories.ErrCategoryNotFound
	}
	if r.inUse[categoryID] {
		return repositories.ErrCategoryInUse
	}
	delete(r.categories, categoryID)
	return nil
}

// ---- fakeTransactionRepo ----

type fakeTransactionRepo struct {
	errInjector
	transactions map[int]*models.Transaction
	nextID       int
}

func newFakeTransactionRepo() *fakeTransactionRepo {
	return &fakeTransactionRepo{
		errInjector:  newErrInjector(),
		transactions: map[int]*models.Transaction{},
		nextID:       1,
	}
}

func (r *fakeTransactionRepo) seed(tx *models.Transaction) {
	r.transactions[tx.ID] = tx
	if tx.ID >= r.nextID {
		r.nextID = tx.ID + 1
	}
}

func (r *fakeTransactionRepo) Create(input models.CreateTransactionInput) error {
	if err := r.injected("Create"); err != nil {
		return err
	}
	tx := &models.Transaction{
		ID:              r.nextID,
		AccountID:       input.AccountID,
		CategoryID:      input.CategoryID,
		Amount:          input.Amount,
		Type:            input.Type,
		Description:     input.Description,
		TransactionDate: input.TransactionDate,
		CreatedAt:       time.Now(),
	}
	r.transactions[r.nextID] = tx
	r.nextID++
	return nil
}

func (r *fakeTransactionRepo) GetByID(transactionID int) (*models.Transaction, error) {
	if err := r.injected("GetByID"); err != nil {
		return nil, err
	}
	tx, ok := r.transactions[transactionID]
	if !ok {
		return nil, repositories.ErrTransactionNotFound
	}
	return tx, nil
}

func (r *fakeTransactionRepo) ListByAccount(accountID int) ([]models.Transaction, error) {
	if err := r.injected("ListByAccount"); err != nil {
		return nil, err
	}
	var out []models.Transaction
	for _, tx := range r.transactions {
		if tx.AccountID == accountID {
			out = append(out, *tx)
		}
	}
	return out, nil
}

func (r *fakeTransactionRepo) CountByCategory(categoryID int) (int64, error) {
	if err := r.injected("CountByCategory"); err != nil {
		return 0, err
	}
	var n int64
	for _, tx := range r.transactions {
		if tx.CategoryID == categoryID {
			n++
		}
	}
	return n, nil
}

// SumExpenseByCategory — gerçek SQL'in mantığını BİREBİR yansıtır: sadece
// expense, sadece verilen hesaplar, yarı açık [from, to) aralığı. Aksi halde
// sınır testleri sorguyu değil sahteyi test etmiş olur.
func (r *fakeTransactionRepo) SumExpenseByCategory(accountIDs []int, from, to time.Time) (map[int]float64, error) {
	if err := r.injected("SumExpenseByCategory"); err != nil {
		return nil, err
	}
	sums := map[int]float64{}
	if len(accountIDs) == 0 {
		return sums, nil
	}
	allowed := map[int]bool{}
	for _, id := range accountIDs {
		allowed[id] = true
	}
	for _, tx := range r.transactions {
		if !allowed[tx.AccountID] || tx.Type != "expense" {
			continue
		}
		d := models.CivilDate(tx.TransactionDate)
		if d.Before(models.CivilDate(from)) || !d.Before(models.CivilDate(to)) {
			continue
		}
		sums[tx.CategoryID] += tx.Amount
	}
	return sums, nil
}

func (r *fakeTransactionRepo) Update(transactionID int, input models.UpdateTransactionInput) error {
	if err := r.injected("Update"); err != nil {
		return err
	}
	tx, ok := r.transactions[transactionID]
	if !ok {
		return repositories.ErrTransactionNotFound
	}
	tx.CategoryID = input.CategoryID
	tx.Amount = input.Amount
	tx.Type = input.Type
	tx.Description = input.Description
	tx.TransactionDate = input.TransactionDate
	return nil
}

func (r *fakeTransactionRepo) Delete(transactionID int) error {
	if err := r.injected("Delete"); err != nil {
		return err
	}
	if _, ok := r.transactions[transactionID]; !ok {
		return repositories.ErrTransactionNotFound
	}
	delete(r.transactions, transactionID)
	return nil
}

// ---- fakeUserRepo ----

type fakeUserRepo struct {
	errInjector
	users  map[string]*models.User
	nextID int
}

func newFakeUserRepo() *fakeUserRepo {
	return &fakeUserRepo{
		errInjector: newErrInjector(),
		users:       map[string]*models.User{},
		nextID:      1,
	}
}

// seedUser doğrudan bir kullanıcı ekler (login testlerinde hazır hash ile kullanmak için).
func (r *fakeUserRepo) seedUser(username, passwordHash string, role models.Role) *models.User {
	u := &models.User{ID: r.nextID, Username: username, PasswordHash: passwordHash, Role: role}
	r.users[username] = u
	r.nextID++
	return u
}

func (r *fakeUserRepo) Create(username, passwordHash string) error {
	if err := r.injected("Create"); err != nil {
		return err
	}
	if _, exists := r.users[username]; exists {
		return repositories.ErrUsernameTaken
	}
	r.users[username] = &models.User{
		ID:           r.nextID,
		Username:     username,
		PasswordHash: passwordHash,
		Role:         models.RoleClient,
	}
	r.nextID++
	return nil
}

func (r *fakeUserRepo) GetByID(userID int) (*models.User, error) {
	if err := r.injected("GetByID"); err != nil {
		return nil, err
	}
	for _, u := range r.users {
		if u.ID == userID {
			return u, nil
		}
	}
	return nil, repositories.ErrUserNotFound
}

func (r *fakeUserRepo) GetByUsername(username string) (*models.User, error) {
	if err := r.injected("GetByUsername"); err != nil {
		return nil, err
	}
	u, ok := r.users[username]
	if !ok {
		return nil, repositories.ErrUserNotFound
	}
	return u, nil
}

// ---- fakeTokenRepo ----

type fakeTokenRepo struct {
	errInjector
	revoked map[string]bool
}

func newFakeTokenRepo() *fakeTokenRepo {
	return &fakeTokenRepo{
		errInjector: newErrInjector(),
		revoked:     map[string]bool{},
	}
}

func (r *fakeTokenRepo) Revoke(jti string, expiresAt time.Time) error {
	if err := r.injected("Revoke"); err != nil {
		return err
	}
	r.revoked[jti] = true
	return nil
}

func (r *fakeTokenRepo) DeleteExpired(before time.Time) (int64, error) {
	if err := r.injected("DeleteExpired"); err != nil {
		return 0, err
	}
	return 0, nil
}

func (r *fakeTokenRepo) IsRevoked(jti string) (bool, error) {
	if err := r.injected("IsRevoked"); err != nil {
		return false, err
	}
	return r.revoked[jti], nil
}

// ---- test yardımcıları ----

// authAs, gerçek AuthMiddleware'in context'e koyduğu değerleri taklit eder.
// Böylece handler testleri JWT üretmeden, doğrudan "giriş yapmış kullanıcı"
// senaryosunu kurabilir.
func authAs(userID int, role models.Role) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("user_id", userID)
		c.Set("role", role)
		c.Set("jti", "test-jti")
		c.Set("token_exp", time.Now().Add(time.Hour))
		c.Next()
	}
}

func performRequest(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ---- fakeRefreshRepo ----
//
// Gerçek repo'nun ATOMİK Consume davranışını taklit eder: bir token yalnızca
// bir kez tüketilebilir, ikinci deneme ErrRefreshTokenReused döner.

type fakeRefreshRepo struct {
	errInjector
	tokens map[string]*models.RefreshToken
	nextID int
}

func newFakeRefreshRepo() *fakeRefreshRepo {
	return &fakeRefreshRepo{
		errInjector: newErrInjector(),
		tokens:      map[string]*models.RefreshToken{},
		nextID:      1,
	}
}

func (r *fakeRefreshRepo) Create(token *models.RefreshToken) error {
	if err := r.injected("Create"); err != nil {
		return err
	}
	token.ID = r.nextID
	r.nextID++
	copy := *token
	r.tokens[token.TokenHash] = &copy
	return nil
}

func (r *fakeRefreshRepo) Consume(tokenHash string, now time.Time) (*models.RefreshToken, error) {
	if err := r.injected("Consume"); err != nil {
		return nil, err
	}
	t, ok := r.tokens[tokenHash]
	if !ok {
		return nil, repositories.ErrRefreshTokenInvalid
	}
	if t.UsedAt != nil {
		// Sızıntı: kaydı DA döndür ki çağıran tüm oturumları iptal edebilsin.
		return t, repositories.ErrRefreshTokenReused
	}
	if t.RevokedAt != nil || !now.Before(t.ExpiresAt) {
		return nil, repositories.ErrRefreshTokenInvalid
	}
	t.UsedAt = &now
	return t, nil
}

func (r *fakeRefreshRepo) Revoke(tokenHash string, now time.Time) error {
	if err := r.injected("Revoke"); err != nil {
		return err
	}
	if t, ok := r.tokens[tokenHash]; ok && t.RevokedAt == nil {
		t.RevokedAt = &now
	}
	return nil
}

func (r *fakeRefreshRepo) RevokeAllForUser(userID int, now time.Time) error {
	if err := r.injected("RevokeAllForUser"); err != nil {
		return err
	}
	for _, t := range r.tokens {
		if t.UserID == userID && t.RevokedAt == nil {
			t.RevokedAt = &now
		}
	}
	return nil
}

func (r *fakeRefreshRepo) DeleteExpired(before time.Time) (int64, error) {
	if err := r.injected("DeleteExpired"); err != nil {
		return 0, err
	}
	var n int64
	for h, t := range r.tokens {
		if t.ExpiresAt.Before(before) {
			delete(r.tokens, h)
			n++
		}
	}
	return n, nil
}

// ---- fakePendingRepo ----
//
// Gerçek repo'nun ATOMİK Claim davranışını taklit eder: token tek kullanımlık,
// süreli ve kullanıcıya bağlı. Dört red sebebi de AYNI hatayı döner —
// gerçekte olduğu gibi (bilgi sızıntısını önlemek için).

type fakePendingRepo struct {
	errInjector
	actions map[string]*models.PendingAction
}

func newFakePendingRepo() *fakePendingRepo {
	return &fakePendingRepo{
		errInjector: newErrInjector(),
		actions:     map[string]*models.PendingAction{},
	}
}

func (r *fakePendingRepo) Create(action *models.PendingAction) error {
	if err := r.injected("Create"); err != nil {
		return err
	}
	cp := *action
	r.actions[action.Token] = &cp
	return nil
}

func (r *fakePendingRepo) Claim(userID int, token string, now time.Time) (*models.PendingAction, error) {
	if err := r.injected("Claim"); err != nil {
		return nil, err
	}
	a, ok := r.actions[token]
	if !ok || a.UserID != userID || a.UsedAt != nil || !now.Before(a.ExpiresAt) {
		return nil, repositories.ErrPendingActionInvalid
	}
	a.UsedAt = &now
	return a, nil
}

func (r *fakePendingRepo) DeleteExpired(before time.Time) (int64, error) {
	if err := r.injected("DeleteExpired"); err != nil {
		return 0, err
	}
	var n int64
	for tok, a := range r.actions {
		if a.ExpiresAt.Before(before) {
			delete(r.actions, tok)
			n++
		}
	}
	return n, nil
}

// ---- fakeActionParser ----
//
// AI'ı taklit eder. Testler modelin NE döndürdüğünü tam olarak kontrol eder;
// gerçek API çağrısı yok, para harcanmaz, sonuç deterministiktir.
//
// Bu, "modelin uydurduğu çıktıya karşı savunmalarımız çalışıyor mu"
// sorusunu test edilebilir kılan şey.

type fakeActionParser struct {
	actions []models.ParsedAction
	err     error
	calls   int
}

func (p *fakeActionParser) Parse(_ context.Context, _ ai.ParseInput) ([]models.ParsedAction, error) {
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	return p.actions, nil
}

// ---- fakeBudgetRepo ----
//
// Gerçek repo'nun davranışını yansıtır: kullanıcı başına tek bütçe, başlık ve
// satırlar birlikte yaşar, Replace tam değiştirmedir.

type fakeBudgetRepo struct {
	errInjector
	budgets map[int]*models.Budget
	lines   map[int][]models.BudgetCategory
	nextID  int
}

func newFakeBudgetRepo() *fakeBudgetRepo {
	return &fakeBudgetRepo{
		errInjector: newErrInjector(),
		budgets:     map[int]*models.Budget{},
		lines:       map[int][]models.BudgetCategory{},
		nextID:      1,
	}
}

func (r *fakeBudgetRepo) seed(b *models.Budget, lines []models.BudgetCategory) {
	r.budgets[b.ID] = b
	r.lines[b.ID] = lines
	if b.ID >= r.nextID {
		r.nextID = b.ID + 1
	}
}

func (r *fakeBudgetRepo) Create(userID int, input models.CreateBudgetInput, startDate time.Time) error {
	if err := r.injected("Create"); err != nil {
		return err
	}
	for _, b := range r.budgets {
		if b.UserID == userID {
			return repositories.ErrBudgetExists
		}
	}
	id := r.nextID
	r.budgets[id] = &models.Budget{
		ID:         id,
		UserID:     userID,
		Name:       input.Name,
		StartDate:  models.CivilDate(startDate),
		PeriodDays: input.PeriodDays,
	}
	r.lines[id] = fakeLinesFor(id, input.Categories)
	r.nextID++
	return nil
}

func fakeLinesFor(budgetID int, inputs []models.BudgetCategoryInput) []models.BudgetCategory {
	out := make([]models.BudgetCategory, 0, len(inputs))
	for i, in := range inputs {
		out = append(out, models.BudgetCategory{
			ID:          i + 1,
			BudgetID:    budgetID,
			CategoryID:  in.CategoryID,
			LimitAmount: in.LimitAmount,
		})
	}
	return out
}

func (r *fakeBudgetRepo) GetForUser(userID int) (*models.Budget, error) {
	if err := r.injected("GetForUser"); err != nil {
		return nil, err
	}
	for _, b := range r.budgets {
		if b.UserID == userID {
			return b, nil
		}
	}
	return nil, repositories.ErrBudgetNotFound
}

func (r *fakeBudgetRepo) ListCategories(budgetID int) ([]models.BudgetCategory, error) {
	if err := r.injected("ListCategories"); err != nil {
		return nil, err
	}
	return r.lines[budgetID], nil
}

func (r *fakeBudgetRepo) Replace(budgetID int, input models.UpdateBudgetInput, startDate time.Time) error {
	if err := r.injected("Replace"); err != nil {
		return err
	}
	b, ok := r.budgets[budgetID]
	if !ok {
		return repositories.ErrBudgetNotFound
	}
	b.Name = input.Name
	b.StartDate = models.CivilDate(startDate)
	b.PeriodDays = input.PeriodDays
	r.lines[budgetID] = fakeLinesFor(budgetID, input.Categories)
	return nil
}

func (r *fakeBudgetRepo) Delete(budgetID int) error {
	if err := r.injected("Delete"); err != nil {
		return err
	}
	if _, ok := r.budgets[budgetID]; !ok {
		return repositories.ErrBudgetNotFound
	}
	delete(r.budgets, budgetID)
	delete(r.lines, budgetID)
	return nil
}

func (r *fakeBudgetRepo) CountByCategory(categoryID int) (int64, error) {
	if err := r.injected("CountByCategory"); err != nil {
		return 0, err
	}
	var n int64
	for _, lines := range r.lines {
		for _, line := range lines {
			if line.CategoryID == categoryID {
				n++
			}
		}
	}
	return n, nil
}
