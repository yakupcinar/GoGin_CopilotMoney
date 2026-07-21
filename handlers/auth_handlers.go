package handlers

import (
	"GoGinMoneyCopilot/auth"
	"GoGinMoneyCopilot/models"
	"GoGinMoneyCopilot/repositories"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("dummy-password-for-timing"), bcrypt.DefaultCost)

type AuthHandler struct {
	users  repositories.UserRepository
	tokens repositories.TokenRepository
}

func NewAuthHandler(users repositories.UserRepository, tokens repositories.TokenRepository) *AuthHandler {
	return &AuthHandler{users: users, tokens: tokens}
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
	if err := c.ShouldBind(&input); err != nil {
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

	token, err := auth.GenerateToken(user.ID, user.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Token couldn't be created"})
		return
	}

	c.JSON(http.StatusOK, models.LoginResponse{Token: token})
}

func (h *AuthHandler) Logout(c *gin.Context) {
	jti := c.MustGet("jti").(string)
	exp := c.MustGet("token_exp").(time.Time)

	if err := h.tokens.Revoke(jti, exp); err != nil {
		respondInternalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Logged out"})
}
