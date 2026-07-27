package middleware

import "github.com/gin-gonic/gin"

// SetupTrustedProxies — ClientIP()'nin hangi kaynağa güveneceğini belirler.
//
// NEDEN AYRI BİR FONKSİYON: rate limiting'in tamamı ClientIP()'nin doğruluğuna
// bağlı. Bu ayar main.go içinde tek satır olsaydı test edilemezdi; burada
// olunca hem üretim yolu hem test aynı kodu çalıştırır.
//
// SORUN: gin varsayılan olarak TÜM vekillere güvenir ve X-Forwarded-For
// başlığını okur. Önümüzde vekil yokken bu başlığı İSTEMCİNİN KENDİSİ yazar —
// yani her isteğe farklı bir değer koyarak IP bazlı limit tamamen atlatılır.
// (Gözlemlendi: 8 istek, hiç 429 yok.)
//
// nil = hiçbir vekile güvenme. ClientIP() başlığı yok sayar ve gerçek TCP
// bağlantı adresini döner; istemci bunu değiştiremez.
//
// ÖNÜNE NGINX/ALB KOYULURSA burası değişmeli: o zaman uygulama yalnızca
// vekilin IP'sini görür ve tüm kullanıcılar tek sayaca düşer. Yalnızca vekilin
// ağına güvenilmeli, örn. []string{"172.16.0.0/12"} (docker ağı).
func SetupTrustedProxies(r *gin.Engine) error {
	return r.SetTrustedProxies(nil)
}
