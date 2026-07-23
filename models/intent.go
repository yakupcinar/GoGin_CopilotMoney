package models

// Intent — chat üzerinden istenebilecek işlemlerin TAM listesi.
//
// Bu liste bir beyaz listedir: modelin ürettiği niyet burada yoksa çalışmaz.
// Güvenlik "modelin iyi davranmasına" değil, bu sonlu listeye dayanır.
//
// auth işlemleri (register/login/logout) BİLEREK yok — kimlik doğrulama
// asla AI üzerinden tetiklenmez.
type Intent string

const (
	// okuma
	IntentListCategories   Intent = "list_categories"
	IntentGetAccount       Intent = "get_account"
	IntentGetTransaction   Intent = "get_transaction"
	IntentListTransactions Intent = "list_transactions"
	IntentBudgetView       Intent = "budget_view"

	// oluşturma
	IntentCreateAccount     Intent = "create_account"
	IntentCreateCategory    Intent = "create_category"
	IntentCreateTransaction Intent = "create_transaction"

	// değiştirme
	IntentUpdateAccount     Intent = "update_account"
	IntentUpdateCategory    Intent = "update_category"
	IntentUpdateTransaction Intent = "update_transaction"

	// silme
	IntentDeleteAccount     Intent = "delete_account"
	IntentDeleteCategory    Intent = "delete_category"
	IntentDeleteTransaction Intent = "delete_transaction"

	// model ne istendiğini anlayamadıysa
	IntentUnknown Intent = "unknown"
)

// Risk — işlemin ne kadar geri alınabilir olduğu. Akışı bu belirler:
//
//	read        -> doğrudan çalıştır
//	create      -> taslak üret, kullanıcı onaylasın
//	destructive -> taslak + onay kodu + AÇIK onay + onay anında yeniden doğrulama
type Risk string

const (
	RiskRead        Risk = "read"
	RiskCreate      Risk = "create"
	RiskDestructive Risk = "destructive"
)

// intentRisks — beyaz listenin kendisi.
var intentRisks = map[Intent]Risk{
	IntentListCategories:   RiskRead,
	IntentGetAccount:       RiskRead,
	IntentGetTransaction:   RiskRead,
	IntentListTransactions: RiskRead,
	IntentBudgetView:       RiskRead,

	IntentCreateAccount:     RiskCreate,
	IntentCreateCategory:    RiskCreate,
	IntentCreateTransaction: RiskCreate,

	IntentUpdateAccount:     RiskDestructive,
	IntentUpdateCategory:    RiskDestructive,
	IntentUpdateTransaction: RiskDestructive,

	IntentDeleteAccount:     RiskDestructive,
	IntentDeleteCategory:    RiskDestructive,
	IntentDeleteTransaction: RiskDestructive,
}

// RiskOf — niyetin riskini döner. İkinci dönüş false ise niyet
// beyaz listede yoktur ve ÇALIŞTIRILMAMALIDIR.
func RiskOf(i Intent) (Risk, bool) {
	r, ok := intentRisks[i]
	return r, ok
}

// AllowedIntents — modele sunulacak niyet listesi (JSON şemasındaki enum).
func AllowedIntents() []string {
	out := make([]string, 0, len(intentRisks)+1)
	for i := range intentRisks {
		out = append(out, string(i))
	}
	out = append(out, string(IntentUnknown))
	return out
}

// ActionParams — tüm niyetlerin parametreleri tek düz nesnede.
//
// Niye tek nesne? Niyete göre değişen (discriminated union) bir JSON şeması
// kurmak mümkün ama açık modeller bunu sık bozuyor. Düz nesne + Go tarafında
// niyete göre doğrulama daha dayanıklı.
type ActionParams struct {
	// TargetRef — kullanıcının kullandığı ifade ("Balık", "market kategorisi").
	// Silme/değiştirmede model ID VERMEZ; ID'yi biz çözeriz ki uydurmasın.
	TargetRef string `json:"target_ref"`
	// TargetID — kullanıcı AÇIKÇA numara söylediyse ("7 numaralı işlemi sil").
	TargetID *int `json:"target_id"`

	// hesap / kategori
	Name         string `json:"name"`
	CategoryType string `json:"category_type"` // "income" | "expense"

	// işlem
	Amount          float64 `json:"amount"`
	Type            string  `json:"type"`
	Description     string  `json:"description"`
	CategoryID      *int    `json:"category_id"`
	TransactionDate string  `json:"transaction_date"`
}

// ParsedAction — modelin ürettiği HAM öneri. Doğrulanmadan asla kullanılmaz.
type ParsedAction struct {
	Intent     Intent       `json:"intent"`
	Params     ActionParams `json:"params"`
	Confidence float64      `json:"confidence"`
	Warnings   []string     `json:"warnings"`
}
