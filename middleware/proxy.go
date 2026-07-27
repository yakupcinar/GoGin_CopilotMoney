package middleware

import (
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

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
// VEKİL ARKASINDAYKEN: TRUSTED_PROXIES ortam değişkeni (virgülle ayrılmış
// CIDR listesi) verilir. Aksi hâlde uygulama yalnızca vekilin IP'sini görür
// ve TÜM kullanıcılar tek sayaca düşer — limit ters yönde bozulur.
//
// Güvenlik, vekilin başlığı ÜZERİNE YAZMASINA dayanır. nginx.conf'ta
// $remote_addr kullanılıyor; $proxy_add_x_forwarded_for kullanılsaydı
// istemcinin sahte değeri listenin başında kalır ve açık geri gelirdi.
func SetupTrustedProxies(r *gin.Engine) error {
	raw := strings.TrimSpace(os.Getenv("TRUSTED_PROXIES"))
	if raw == "" {
		// Vekil yok: başlığa güvenme, gerçek TCP adresini kullan.
		return r.SetTrustedProxies(nil)
	}

	var cidrs []string
	for _, p := range strings.Split(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			cidrs = append(cidrs, p)
		}
	}
	return r.SetTrustedProxies(cidrs)
}
