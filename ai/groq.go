package ai

// Groq implementasyonu — SDK YOK, düz net/http.
//
// Groq OpenAI-uyumlu bir API sunar. Aynı kod OpenRouter, Together, Ollama
// gibi diğer OpenAI-uyumlu sağlayıcılarda da çalışır: sadece GROQ_BASE_URL
// ve GROQ_MODEL değerlerini değiştirmen yeterli.
//
// Ortam değişkenleri:
//   GROQ_API_KEY   (zorunlu)
//   GROQ_MODEL     (opsiyonel, varsayılan aşağıda)
//   GROQ_BASE_URL  (opsiyonel)

import (
	"GoGinMoneyCopilot/models"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	defaultGroqBaseURL = "https://api.groq.com/openai/v1"
	// Model kimlikleri zamanla değişir: https://console.groq.com/docs/models
	defaultGroqModel = "llama-3.3-70b-versatile"
)

type groqParser struct {
	apiKey  string
	model   string
	baseURL string
	http    *http.Client
}

func NewGroqParser() (ActionParser, error) {
	key := os.Getenv("GROQ_API_KEY")
	if key == "" {
		return nil, errors.New("GROQ_API_KEY ayarlı değil")
	}
	model := os.Getenv("GROQ_MODEL")
	if model == "" {
		model = defaultGroqModel
	}
	baseURL := os.Getenv("GROQ_BASE_URL")
	if baseURL == "" {
		baseURL = defaultGroqBaseURL
	}
	return &groqParser{
		apiKey:  key,
		model:   model,
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{}, // zaman aşımı context ile yönetiliyor
	}, nil
}

// --- OpenAI chat-completions istek/cevap gövdeleri ---

type chatMessage struct {
	Role    string `json:"role"` // "system" | "user" | "assistant"
	Content string `json:"content"`
}

type chatRequest struct {
	Model          string        `json:"model"`
	Messages       []chatMessage `json:"messages"`
	MaxTokens      int           `json:"max_tokens"`
	Temperature    float64       `json:"temperature"`
	ResponseFormat struct {
		Type string `json:"type"`
	} `json:"response_format"`
}

type chatResponse struct {
	Choices []struct {
		Message      chatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// Parse — 429 (rate limit) durumunda bekleyip yeniden dener.
// Ücretsiz katmanlarda dakikalık token limiti kolayca dolar; API bize ne
// kadar bekleyeceğimizi söylüyor, ona uyuyoruz.
func (p *groqParser) Parse(ctx context.Context, in ParseInput) ([]models.ParsedAction, error) {
	const maxAttempts = 3

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		actions, retryAfter, err := p.parseOnce(ctx, in)
		if err == nil {
			return actions, nil
		}
		lastErr = err

		if retryAfter <= 0 || attempt == maxAttempts {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(retryAfter):
		}
	}
	return nil, lastErr
}

// parseOnce — tek deneme. İkinci dönüş: rate limit ise ne kadar beklenecek.
func (p *groqParser) parseOnce(ctx context.Context, in ParseInput) ([]models.ParsedAction, time.Duration, error) {
	reqBody := chatRequest{
		Model: p.model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt()},
			{Role: "user", Content: buildUserPrompt(in)},
		},
		MaxTokens: 1024,
		// 0 = mümkün olduğunca deterministik. Çıkarım işinde yaratıcılık istemiyoruz.
		// (Yine de tam determinizm GARANTİ DEĞİL — GPU/batch farkları var.)
		Temperature: 0,
	}
	// JSON modu: tek garantisi "çıktı geçerli JSON olacak". Şemaya uyum garantisi YOK.
	reqBody.ResponseFormat.Type = "json_object"

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("istek gövdesi hazırlanamadı: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, 0, fmt.Errorf("istek oluşturulamadı: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("API'ye ulaşılamadı: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("cevap okunamadı: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		wait := retryAfterFrom(resp, body)
		return nil, wait, fmt.Errorf("rate limit (HTTP 429), %v sonra tekrar denenecek", wait)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("API hata döndü (HTTP %d): %s",
			resp.StatusCode, truncateForError(string(body)))
	}

	var cr chatResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return nil, 0, fmt.Errorf("cevap zarfı çözülemedi: %w", err)
	}
	if cr.Error != nil {
		return nil, 0, fmt.Errorf("API hatası: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return nil, 0, errors.New("modelden boş cevap geldi")
	}
	if cr.Choices[0].FinishReason == "length" {
		return nil, 0, errors.New("cevap max_tokens sınırında kesildi")
	}

	// Açık modeller JSON modunda bile bazen ```json ... ``` ile sarmalıyor.
	content := stripCodeFence(cr.Choices[0].Message.Content)

	var wrapper struct {
		Actions []models.ParsedAction `json:"actions"`
	}
	if err := json.Unmarshal([]byte(content), &wrapper); err != nil {
		return nil, 0, fmt.Errorf("model çıktısı JSON olarak okunamadı: %w (ham: %s)",
			err, truncateForError(content))
	}
	if len(wrapper.Actions) == 0 {
		return nil, 0, errors.New("model hiç eylem döndürmedi")
	}
	return wrapper.Actions, 0, nil
}

var retryHintRe = regexp.MustCompile(`try again in ([0-9.]+)s`)

// retryAfterFrom — önce retry-after başlığına, yoksa hata metnindeki
// "try again in 5.6s" ifadesine bakar.
func retryAfterFrom(resp *http.Response, body []byte) time.Duration {
	if v := resp.Header.Get("retry-after"); v != "" {
		if secs, err := strconv.ParseFloat(v, 64); err == nil && secs > 0 {
			return time.Duration(secs*1000) * time.Millisecond
		}
	}
	if m := retryHintRe.FindSubmatch(body); m != nil {
		if secs, err := strconv.ParseFloat(string(m[1]), 64); err == nil && secs > 0 {
			return time.Duration(secs*1000)*time.Millisecond + 250*time.Millisecond
		}
	}
	return 3 * time.Second
}

func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	if i := strings.Index(s, "\n"); i != -1 {
		s = s[i+1:]
	}
	if i := strings.LastIndex(s, "```"); i != -1 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func truncateForError(s string) string {
	const max = 300
	if r := []rune(s); len(r) > max {
		return string(r[:max]) + "..."
	}
	return s
}

// Derleme zamanı kontrolü: groqParser gerçekten ActionParser'ı karşılıyor mu?
var _ ActionParser = (*groqParser)(nil)
