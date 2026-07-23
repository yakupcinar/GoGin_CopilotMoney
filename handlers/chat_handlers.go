package handlers

import (
	"GoGinMoneyCopilot/chat"
	"GoGinMoneyCopilot/models"
	"GoGinMoneyCopilot/repositories"
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

// ChatHandler — chat akışının İNCE HTTP katmanı.
//
// Buradaki tek iş: isteği okumak, kimliği context'ten almak, servisi çağırmak,
// hataları doğru HTTP koduna çevirmek. İş mantığı YOK — o chat/ paketinde.
//
// service nil olabilir: GROQ_API_KEY ayarlı değilse chat özelliği kapalıdır.
// Route'u hiç kaydetmemek yerine 503 dönmeyi tercih ediyoruz — 404 alan
// geliştirici "route'u mu unuttum?" diye arar, 503 sebebi açıkça söyler.
type ChatHandler struct {
	service *chat.ActionService
}

func NewChatHandler(service *chat.ActionService) *ChatHandler {
	return &ChatHandler{service: service}
}

type chatRequestBody struct {
	Text string `json:"text" binding:"required,max=500"`
	// AccountID opsiyonel: işlem oluştururken hesap belirtilmemişse kullanılır.
	AccountID int `json:"account_id"`
}

type confirmRequestBody struct {
	Token string `json:"token" binding:"required,max=64"`
}

// Chat — POST /chat
func (h *ChatHandler) Chat(c *gin.Context) {
	if h.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Chat feature is not configured (GROQ_API_KEY missing)"})
		return
	}

	var body chatRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input format!"})
		return
	}

	// Kimlik context'ten, gövdeden DEĞİL.
	userID := c.MustGet("user_id").(int)
	role := c.MustGet("role").(models.Role)

	results, err := h.service.Chat(c.Request.Context(), chat.ChatRequest{
		UserID:           userID,
		Role:             role,
		DefaultAccountID: body.AccountID,
		Text:             body.Text,
	})
	if err != nil {
		h.respondChatError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"results": results})
}

// Confirm — POST /actions/confirm
// Frontend'deki "Evet, eminim" butonunun gittiği yer.
func (h *ChatHandler) Confirm(c *gin.Context) {
	if h.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Chat feature is not configured (GROQ_API_KEY missing)"})
		return
	}

	var body confirmRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input format!"})
		return
	}

	userID := c.MustGet("user_id").(int)

	message, err := h.service.Confirm(userID, body.Token)
	if err != nil {
		h.respondConfirmError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": message})
}

// respondChatError — Chat akışının hataları.
func (h *ChatHandler) respondChatError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, chat.ErrEmptyText), errors.Is(err, chat.ErrTextTooLong):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		// AI servisine ulaşılamadı / rate limit / bozuk cevap.
		// Bu bizim hatamız değil, DIŞ BİR BAĞIMLILIĞIN sorunu -> 503.
		// 500 deseydik "uygulama bozuk" sinyali verirdik; 503 "şu an
		// hizmet veremiyorum, sonra dene" demek.
		//
		// Detayı log'a yazıyoruz ama client'a vermiyoruz: hata metninde
		// prompt parçası veya API anahtarı izleri olabilir.
		log.Println("chat error:", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "AI service is temporarily unavailable, please try again"})
	}
}

// respondConfirmError — Confirm akışının hataları.
func (h *ChatHandler) respondConfirmError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, repositories.ErrPendingActionInvalid):
		// Yok / başkasının / kullanılmış / süresi dolmuş — HEPSİ aynı cevap.
		// Ayırsaydık başkasının token'ının varlığını sızdırırdık.
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Confirmation token is invalid or expired"})

	case errors.Is(err, repositories.ErrCategoryInUse):
		c.JSON(http.StatusConflict, gin.H{
			"error": "This category is used by existing transactions and cannot be deleted"})

	case errors.Is(err, repositories.ErrAccountInUse):
		c.JSON(http.StatusConflict, gin.H{
			"error": "This account has existing transactions and cannot be deleted"})

	case errors.Is(err, repositories.ErrCategoryNotFound),
		errors.Is(err, repositories.ErrAccountNotFound),
		errors.Is(err, repositories.ErrTransactionNotFound),
		errors.Is(err, repositories.ErrBudgetNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "Target record not found"})

	case errors.Is(err, chat.ErrGlobalCategory):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})

	default:
		// Kullanıcıya gösterilebilir doğrulama hatası mı?
		// (örn. "kategori yeni işlem tipiyle uyuşmuyor") -> 400, sebebiyle.
		var ve *chat.ValidationError
		if errors.As(err, &ve) {
			c.JSON(http.StatusBadRequest, gin.H{"error": ve.Msg})
			return
		}
		respondInternalError(c, err)
	}
}
