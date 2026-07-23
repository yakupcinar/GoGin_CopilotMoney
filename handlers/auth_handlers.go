package handlers

import (
	"GoGinMoneyCopilot/auth"
	"GoGinMoneyCopilot/models"
	"GoGinMoneyCopilot/repositories"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("dummy-password-for-timing"), bcrypt.DefaultCost)

// AuthHandler — hibrit token akışı.
//
//	access token  : 15 dk, JSON gövdesinde döner, frontend BELLEKTE tutar,
//	                Authorization header ile taşınır
//	refresh token : 7 gün, HttpOnly cookie (Path=/auth), JS okuyamaz
type AuthHandler struct {
	users   repositories.UserRepository
	tokens  repositories.TokenRepository        // access token kara listesi (jti)
	refresh repositories.RefreshTokenRepository // oturum kayıtları
}

func NewAuthHandler(
	users repositories.UserRepository,
	tokens repositories.TokenRepository,
	refresh repositories.RefreshTokenRepository,
) *AuthHandler {
	return &AuthHandler{users: users, tokens: tokens, refresh: refresh}
}

func (h *AuthHandler) Register(c *gin.Context) {
	var input models.RegisterInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input format!"})
		return
	}

	hashedPassword, err := auth.HashPassword(input.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Password couldn't made"})
		return
	}
	if err := h.users.Create(input.Username, hashedPassword); err != nil {
		if errors.Is(err, repositories.ErrUsernameTaken) {
			c.JSON(http.StatusConflict, gin.H{"error": "Username Already Exist!"})
			return
		}
		respondInternalError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "Register succesful!"})
}

func (h *AuthHandler) Login(c *gin.Context) {
	var input models.LoginInput
	// ShouldBind DEĞİL ShouldBindJSON: ShouldBind form-encoded veriyi de kabul
	// ederdi. Cookie tabanlı kimliğe geçtiğimiz için "form kabul eden endpoint"
	// kalıbı artık risk taşıyor — cross-site bir HTML formu JSON gönderemez,
	// bu da fazladan bir yapısal engel.
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input format!"})
		return
	}

	user, err := h.users.GetByUsername(input.Username)
	if err != nil {
		// Altyapı hatasını (DB erişilemiyor vb.) "şifre yanlış" gibi göstermeyelim:
		// yalnızca gerçekten kullanıcı yoksa 401 dön.
		if !errors.Is(err, repositories.ErrUserNotFound) {
			respondInternalError(c, err)
			return
		}
		// Kullanıcı yoksa da bcrypt'i çalıştır ki iki hata yolu aynı sürsün
		// (kullanıcı adı enumeration'ını engelleyen timing koruması).
		auth.CheckPassword(input.Password, string(dummyHash))
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Username or Password is wrong!"})
		return
	}

	if !auth.CheckPassword(input.Password, user.PasswordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Username or Password is wrong!"})
		return
	}

	h.issueTokenPair(c, user)
}

// Refresh — POST /auth/refresh
//
// Access token'ın süresi dolduğunda çağrılır. Gövde BOŞ: kimlik yalnızca
// HttpOnly cookie'den gelir.
//
// Bu endpoint AuthMiddleware'in ARKASINDA OLAMAZ — zaten access token'ın
// süresi dolduğu için buradayız.
func (h *AuthHandler) Refresh(c *gin.Context) {
	raw := auth.RefreshTokenFromRequest(c)
	if raw == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Session not found, please log in"})
		return
	}

	now := time.Now()
	record, err := h.refresh.Consume(auth.HashRefreshToken(raw), now)
	if err != nil {
		h.handleRefreshFailure(c, record, err, now)
		return
	}

	// Rolü TAZE oku: refresh token'ın içine gömseydik, yetkisi alınmış bir
	// kullanıcı token'ı geçerli olduğu sürece eski yetkisini korurdu.
	user, err := h.users.GetByID(record.UserID)
	if err != nil {
		// Kullanıcı silinmiş olabilir — oturumu sonlandır.
		auth.ClearRefreshCookie(c)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Session is no longer valid"})
		return
	}

	h.issueTokenPair(c, user)
}

// handleRefreshFailure — yenileme başarısız. İki durum var, tepkileri farklı.
func (h *AuthHandler) handleRefreshFailure(c *gin.Context, record *models.RefreshToken, err error, now time.Time) {
	if errors.Is(err, repositories.ErrRefreshTokenReused) && record != nil {
		// SIZINTI SİNYALİ: tüketilmiş bir token tekrar sunuldu.
		// Ya saldırgan çaldı ya meşru kullanıcı eskisini oynatıyor — ayırt
		// edemeyiz. Güvenli taraf: o kullanıcının TÜM oturumlarını kapat.
		if revokeErr := h.refresh.RevokeAllForUser(record.UserID, now); revokeErr != nil {
			log.Printf("reuse detected but revocation failed (user=%d): %v",
				record.UserID, revokeErr)
		} else {
			log.Printf("SECURITY: refresh token reused, "+
				"all of the user's sessions were revoked (user=%d)", record.UserID)
		}
	} else if !errors.Is(err, repositories.ErrRefreshTokenInvalid) {
		// Beklenmeyen altyapı hatası — log'a yaz.
		log.Println("refresh error:", err)
	}

	// Client'a HER DURUMDA aynı cevap. Sızıntı mı, süresi mi dolmuş, yok mu —
	// ayırt edilebilirse saldırgan "bu token gerçekti" bilgisini elde eder.
	auth.ClearRefreshCookie(c)
	c.JSON(http.StatusUnauthorized, gin.H{"error": "Session expired, please log in again"})
}

// Logout — POST /auth/logout
//
// Üç iş birden: access token'ı anında iptal et, refresh token'ı iptal et,
// cookie'yi temizle.
func (h *AuthHandler) Logout(c *gin.Context) {
	jti := c.MustGet("jti").(string)
	exp := c.MustGet("token_exp").(time.Time)
	now := time.Now()

	// 1) Access token'ı kara listeye al. 15 dk kısa ama "çıkış yaptım" diyen
	//    kullanıcının token'ı o 15 dk boyunca çalışmaya devam etmemeli.
	if err := h.tokens.Revoke(jti, exp); err != nil {
		respondInternalError(c, err)
		return
	}

	// 2) Refresh token'ı iptal et. Cookie'yi silmek TEK BAŞINA yetmez:
	//    değeri kopyalayan biri onu kullanmaya devam edebilirdi.
	if raw := auth.RefreshTokenFromRequest(c); raw != "" {
		if err := h.refresh.Revoke(auth.HashRefreshToken(raw), now); err != nil {
			log.Println("logout: refresh token iptal edilemedi:", err)
			// Akışı kesmiyoruz — access token zaten iptal edildi.
		}
	}

	// 3) Tarayıcıdan cookie'yi kaldır.
	auth.ClearRefreshCookie(c)

	c.JSON(http.StatusOK, gin.H{"message": "Logged out"})
}

// issueTokenPair — Login ve Refresh'in ortak son adımı.
//
// Access token gövdede döner (frontend bellekte tutar).
// Refresh token cookie'ye yazılır (frontend hiç görmez).
func (h *AuthHandler) issueTokenPair(c *gin.Context, user *models.User) {
	accessToken, err := auth.GenerateToken(user.ID, user.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Token couldn't be created"})
		return
	}

	rawRefresh, hashRefresh, err := auth.NewRefreshToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Session couldn't be created"})
		return
	}

	if err := h.refresh.Create(&models.RefreshToken{
		UserID:    user.ID,
		TokenHash: hashRefresh, // ham değer DB'ye ASLA yazılmaz
		ExpiresAt: time.Now().Add(auth.RefreshTokenTTL()),
	}); err != nil {
		respondInternalError(c, err)
		return
	}

	auth.SetRefreshCookie(c, rawRefresh)
	c.JSON(http.StatusOK, models.LoginResponse{Token: accessToken})
}
