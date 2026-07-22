package models

import "time"

// PendingAction — yıkıcı bir işlem için üretilmiş, onay bekleyen kayıt.
//
// PLAYGROUND'DAN FARK: orada bellekteydi (map). Burada VERİTABANI tablosu,
// çünkü sunucu yeniden başladığında bekleyen onaylar kaybolmamalı — ve
// birden fazla sunucu kopyası çalışıyorsa hepsi aynı kaydı görmeli.
//
// Token'ın taşıdığı garantiler:
//   - tek kullanımlık : UsedAt dolduysa bir daha çalışmaz
//   - süreli          : ExpiresAt geçtiyse çalışmaz
//   - kullanıcıya bağlı: UserID eşleşmezse çalışmaz
//   - hedefi sabit     : onaylanan kayıt ile silinen kayıt aynıdır
type PendingAction struct {
	Token  string `json:"token" gorm:"primaryKey;size:64"`
	UserID int    `json:"user_id" gorm:"not null;index"`
	Intent Intent `json:"intent" gorm:"size:32;not null"`

	// TargetID — üzerinde işlem yapılacak kaydın id'si (kategori/hesap/işlem).
	TargetID int `json:"target_id" gorm:"not null"`

	// Summary — kullanıcıya gösterilecek özet. Frontend'in "Emin misiniz?"
	// popup'ında bu metin yazar. Backend'de üretilir ki chat ve panel
	// aynı şeyi göstersin.
	Summary string `json:"summary" gorm:"size:500"`

	// Params — update işlemlerinde yeni değerler.
	// serializer:json -> GORM bunu tek bir JSON kolonuna yazar/okur.
	Params ActionParams `json:"params" gorm:"serializer:json"`

	ExpiresAt time.Time  `json:"expires_at" gorm:"not null;index"`
	UsedAt    *time.Time `json:"used_at"`
	CreatedAt time.Time  `json:"created_at"`
}

// IsUsable — token'ın çalıştırılabilir olup olmadığı.
// DİKKAT: bu sadece TOKEN'ı doğrular. Hedefin hâlâ var olduğu ve hâlâ
// bu kullanıcıya ait olduğu ayrıca kontrol edilmelidir (TOCTOU).
func (p *PendingAction) IsUsable(now time.Time) bool {
	return p.UsedAt == nil && now.Before(p.ExpiresAt)
}
