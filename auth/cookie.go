package auth

import (
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// Refresh token cookie'sinin yapılandırması ve set/clear yardımcıları.

const (
	// RefreshCookieName — cookie adı.
	RefreshCookieName = "refresh_token"

	// RefreshCookiePath — cookie YALNIZCA bu yol altındaki isteklerde gönderilir.
	//
	// Bu, CSRF yüzeyini tek bir yere indirger: diğer 15 endpoint'e cookie hiç
	// gitmediği için oralarda CSRF yapısal olarak imkânsız hale gelir.
	// Bu yüzden logout da /auth altına taşındı — refresh token'ı görebilmesi
	// gerekiyor ki veritabanından iptal edebilsin.
	RefreshCookiePath = "/auth"

	defaultRefreshTTL = 7 * 24 * time.Hour // 7 gün
	defaultAccessTTL  = 15 * time.Minute
)

// RefreshTokenTTL — REFRESH_TOKEN_TTL ortam değişkeni (örn. "168h").
func RefreshTokenTTL() time.Duration {
	return durationEnv("REFRESH_TOKEN_TTL", defaultRefreshTTL)
}

// AccessTokenTTL — ACCESS_TOKEN_TTL ortam değişkeni (örn. "15m").
//
// Kısa tutulmasının sebebi: access token bellekte durduğu ve header'la
// taşındığı için XSS'e görece açıktır. Ömrü kısaltarak çalınması hâlinde
// kullanılabileceği pencereyi daraltıyoruz.
func AccessTokenTTL() time.Duration {
	return durationEnv("ACCESS_TOKEN_TTL", defaultAccessTTL)
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		log.Printf("%s is invalid (%q), using default: %v", key, v, fallback)
		return fallback
	}
	return d
}

// cookieSecure — COOKIE_SECURE. Üretimde MUTLAKA true olmalı (HTTPS).
// Geliştirmede sunucu HTTP olduğu için varsayılan false.
func cookieSecure() bool {
	return strings.EqualFold(os.Getenv("COOKIE_SECURE"), "true")
}

// cookieSameSite — COOKIE_SAMESITE: strict | lax | none.
//
// strict (varsayılan): cookie cross-site isteklerde HİÇ gönderilmez -> CSRF kapalı.
// lax             : üst düzey GET gezinmelerinde gönderilir.
// none            : her yerde gönderilir; ayrı origin'deki frontend için gerekli
//
//	AMA bu durumda CSRF token'ı ZORUNLU hale gelir.
func cookieSameSite() http.SameSite {
	switch strings.ToLower(os.Getenv("COOKIE_SAMESITE")) {
	case "lax":
		return http.SameSiteLaxMode
	case "none":
		return http.SameSiteNoneMode
	default:
		return http.SameSiteStrictMode
	}
}

func cookieDomain() string {
	return os.Getenv("COOKIE_DOMAIN")
}

// ValidateCookieConfig — başlangıçta çağrılır; tehlikeli kombinasyonları yakalar.
func ValidateCookieConfig() error {
	// Tarayıcılar SameSite=None olan cookie'yi Secure değilse REDDEDER.
	// Bu sessizce başarısız olur — kullanıcı "giriş yapamıyorum" der, sebebi
	// hiçbir yerde görünmez. O yüzden başlangıçta yakalıyoruz.
	if cookieSameSite() == http.SameSiteNoneMode && !cookieSecure() {
		return errSameSiteNoneRequiresSecure
	}
	if cookieSameSite() == http.SameSiteNoneMode {
		log.Println("WARNING: COOKIE_SAMESITE=none — CSRF protection disabled. " +
			"If you use a separate origin, ADD a double-submit CSRF token.")
	}
	if !cookieSecure() {
		log.Println("WARNING: COOKIE_SECURE=false — refresh cookie is sent over plain HTTP " +
			"and must be true in production.")
	}
	return nil
}

var errSameSiteNoneRequiresSecure = &configError{
	"COOKIE_SECURE=true is required when COOKIE_SAMESITE=none (browsers reject the cookie otherwise)"}

type configError struct{ msg string }

func (e *configError) Error() string { return e.msg }

// SetRefreshCookie — ham refresh token'ı HttpOnly cookie olarak yazar.
//
// HttpOnly: JavaScript okuyamaz -> XSS token'ı çalamaz. Hibrit tasarımın
// asıl kazancı burada.
func SetRefreshCookie(c *gin.Context, raw string) {
	// DİKKAT: SetSameSite, SetCookie'den ÖNCE çağrılmalı — gin bu ayarı
	// bir sonraki SetCookie çağrısında uygular.
	c.SetSameSite(cookieSameSite())
	c.SetCookie(
		RefreshCookieName,
		raw,
		int(RefreshTokenTTL().Seconds()),
		RefreshCookiePath,
		cookieDomain(),
		cookieSecure(),
		true, // httpOnly
	)
}

// ClearRefreshCookie — cookie'yi siler (maxAge < 0).
//
// Not: cookie'yi silmek oturumu sonlandırmaya YETMEZ. Saldırgan değeri zaten
// kopyaladıysa cookie'nin tarayıcıdan silinmesi onu durdurmaz. Bu yüzden
// logout ayrıca token'ı veritabanında da iptal eder.
func ClearRefreshCookie(c *gin.Context) {
	c.SetSameSite(cookieSameSite())
	c.SetCookie(
		RefreshCookieName, "", -1,
		RefreshCookiePath, cookieDomain(), cookieSecure(), true,
	)
}

// RefreshTokenFromRequest — cookie'den ham token'ı okur.
func RefreshTokenFromRequest(c *gin.Context) string {
	raw, err := c.Cookie(RefreshCookieName)
	if err != nil {
		return ""
	}
	return raw
}
