package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
)

const (
	kvKeyBotToken = "telegram.bot_token"
	kvKeyChatID   = "telegram.chat_id"
	kvKeyEnabled  = "telegram.enabled"

	apiBase       = "https://api.telegram.org"
	queueCap      = 64
	maxRetries    = 3
	baseRetry     = 2 * time.Second
	sendTimeout   = 10 * time.Second
	parseMode     = "HTML"
	maxMsgLen     = 4096
)

type kvStore interface {
	Get(key string) (string, bool)
	Set(key, value string) error
}

type Config struct {
	BotToken string `json:"botToken"`
	ChatID   string `json:"chatId"`
	Enabled  bool   `json:"enabled"`
}

type Notifier struct {
	kv     kvStore
	queue  chan alerts.AlertEvent
	done   chan struct{}
	wg     sync.WaitGroup
	client *http.Client
}

func LoadConfig(kv kvStore) Config {
	cfg := Config{}
	if v, ok := kv.Get(kvKeyBotToken); ok {
		cfg.BotToken = v
	}
	if v, ok := kv.Get(kvKeyChatID); ok {
		cfg.ChatID = v
	}
	if v, ok := kv.Get(kvKeyEnabled); ok {
		cfg.Enabled = v == "true"
	}
	return cfg
}

func SaveConfig(kv kvStore, cfg Config) error {
	if err := kv.Set(kvKeyBotToken, cfg.BotToken); err != nil {
		return fmt.Errorf("save bot_token: %w", err)
	}
	if err := kv.Set(kvKeyChatID, cfg.ChatID); err != nil {
		return fmt.Errorf("save chat_id: %w", err)
	}
	enabled := "false"
	if cfg.Enabled {
		enabled = "true"
	}
	if err := kv.Set(kvKeyEnabled, enabled); err != nil {
		return fmt.Errorf("save enabled: %w", err)
	}
	return nil
}

func NewNotifier(kv kvStore) *Notifier {
	n := &Notifier{
		kv:     kv,
		queue:  make(chan alerts.AlertEvent, queueCap),
		done:   make(chan struct{}),
		client: &http.Client{Timeout: sendTimeout},
	}
	n.wg.Add(1)
	go n.worker()
	return n
}

func (n *Notifier) Notify(ev alerts.AlertEvent) {
	select {
	case n.queue <- ev:
	default:
		slog.Warn("telegram: queue full, dropping alert", "title", ev.Title)
	}
}

func (n *Notifier) Close() {
	close(n.done)
	n.wg.Wait()
}

func (n *Notifier) worker() {
	defer n.wg.Done()
	for {
		select {
		case <-n.done:
			return
		case ev := <-n.queue:
			cfg := LoadConfig(n.kv)
			if !cfg.Enabled || cfg.BotToken == "" || cfg.ChatID == "" {
				continue
			}
			msg := formatMessage(ev)
			if err := n.sendWithRetry(cfg, msg, ev.Urgent); err != nil {
				slog.Error("telegram: send failed after retries", "error", err, "title", ev.Title)
			}
		}
	}
}

func (n *Notifier) sendWithRetry(cfg Config, text string, urgent bool) error {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoff := baseRetry * time.Duration(1<<(attempt-1))
			if retryAfter, ok := retryAfterFromErr(lastErr); ok && retryAfter > 0 {
				backoff = retryAfter
			}
			select {
			case <-time.After(backoff):
			case <-n.done:
				return fmt.Errorf("shutdown during retry")
			}
		}
		if err := n.sendMessage(cfg, text, urgent); err != nil {
			lastErr = err
			if !isRetryable(err) {
				return err
			}
			continue
		}
		return nil
	}
	return fmt.Errorf("max retries: %w", lastErr)
}

type sendPayload struct {
	ChatID              string `json:"chat_id"`
	Text                string `json:"text"`
	ParseMode           string `json:"parse_mode"`
	DisableNotification bool   `json:"disable_notification,omitempty"`
}

type apiResponse struct {
	OK          bool            `json:"ok"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
	Result      json.RawMessage `json:"result"`
	Parameters  *apiParameters  `json:"parameters,omitempty"`
}

type apiParameters struct {
	RetryAfter int `json:"retry_after,omitempty"`
}

func (n *Notifier) sendMessage(cfg Config, text string, urgent bool) error {
	payload := sendPayload{
		ChatID:              cfg.ChatID,
		Text:                text,
		ParseMode:           parseMode,
		DisableNotification: !urgent,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	url := fmt.Sprintf("%s/bot%s/sendMessage", apiBase, cfg.BotToken)
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	var apiResp apiResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	if !apiResp.OK {
		errMsg := fmt.Sprintf("api error %d: %s", apiResp.ErrorCode, apiResp.Description)
		if apiResp.Parameters != nil && apiResp.Parameters.RetryAfter > 0 {
			errMsg = fmt.Sprintf("%s [retry_after=%d]", errMsg, apiResp.Parameters.RetryAfter)
		}
		return fmt.Errorf("%s", errMsg)
	}
	return nil
}

func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// 4xx (except 429) are not retryable
	for _, code := range []string{"400", "401", "403", "404"} {
		if contains(msg, "api error "+code) {
			return false
		}
	}
	return true
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func formatMessage(ev alerts.AlertEvent) string {
	emoji := categoryEmoji(ev.Category)
	severity := "⚠️"
	if ev.Urgent {
		severity = "🔴"
	}

	ts := time.Unix(ev.Ts, 0).Format("15:04:05")
	text := fmt.Sprintf("%s%s <b>%s</b>\n", severity, emoji, htmlEscape(ev.Title))
	if ev.Description != "" {
		desc := ev.Description
		if len(desc) > 500 {
			desc = desc[:500] + "..."
		}
		text += htmlEscape(desc) + "\n"
	}
	if ev.RouterID != "" {
		text += fmt.Sprintf("📡 %s\n", htmlEscape(ev.RouterID))
	}
	if ev.Hint != "" {
		text += fmt.Sprintf("💡 <i>%s</i>\n", htmlEscape(ev.Hint))
	}
	text += fmt.Sprintf("🕐 %s", ts)

	if len(text) > maxMsgLen {
		text = text[:maxMsgLen-3] + "..."
	}
	return text
}

func categoryEmoji(cat string) string {
	switch cat {
	case "router":
		return "📡"
	case "internet":
		return "🌐"
	case "clients":
		return "📱"
	case "signal":
		return "📶"
	case "vpn":
		return "🔒"
	case "system":
		return "⚙️"
	default:
		return "🔔"
	}
}

func htmlEscape(s string) string {
	r := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<':
			r = append(r, '&', 'l', 't', ';')
		case '>':
			r = append(r, '&', 'g', 't', ';')
		case '&':
			r = append(r, '&', 'a', 'm', 'p', ';')
		default:
			r = append(r, s[i])
		}
	}
	return string(r)
}

func SendTest(kv kvStore) error {
	cfg := LoadConfig(kv)
	if cfg.BotToken == "" || cfg.ChatID == "" {
		return fmt.Errorf("bot token and chat ID are required")
	}
	n := &Notifier{
		client: &http.Client{Timeout: sendTimeout},
		done:   make(chan struct{}),
	}
	defer close(n.done)
	text := "✅ <b>NetPulse</b> Telegram notifications configured successfully."
	return n.sendMessage(cfg, text, true)
}

func ValidateConfig(botToken, chatID string) (botName string, chatTitle string, err error) {
	client := &http.Client{Timeout: sendTimeout}

	// Validate bot token via getMe
	meURL := fmt.Sprintf("%s/bot%s/getMe", apiBase, botToken)
	resp, err := client.Get(meURL)
	if err != nil {
		return "", "", fmt.Errorf("getMe request failed: %w", err)
	}
	defer resp.Body.Close()

	var meResp apiResponse
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if jsonErr := json.Unmarshal(body, &meResp); jsonErr != nil {
		return "", "", fmt.Errorf("getMe: invalid response")
	}
	if !meResp.OK {
		return "", "", fmt.Errorf("invalid bot token: %s", meResp.Description)
	}

	var meResult struct {
		Username  string `json:"username"`
		FirstName string `json:"first_name"`
	}
	if jsonErr := json.Unmarshal(meResp.Result, &meResult); jsonErr == nil {
		botName = meResult.Username
		if botName == "" {
			botName = meResult.FirstName
		}
	}

	// Validate chat ID via getChat
	chatURL := fmt.Sprintf("%s/bot%s/getChat?chat_id=%s", apiBase, botToken, chatID)
	resp2, err := client.Get(chatURL)
	if err != nil {
		return botName, "", fmt.Errorf("getChat request failed: %w", err)
	}
	defer resp2.Body.Close()

	var chatResp apiResponse
	body2, _ := io.ReadAll(io.LimitReader(resp2.Body, 4096))
	if jsonErr := json.Unmarshal(body2, &chatResp); jsonErr != nil {
		return botName, "", fmt.Errorf("getChat: invalid response")
	}
	if !chatResp.OK {
		return botName, "", fmt.Errorf("invalid chat ID: %s", chatResp.Description)
	}

	var chatResult struct {
		Title     string `json:"title"`
		FirstName string `json:"first_name"`
		Type      string `json:"type"`
	}
	if jsonErr := json.Unmarshal(chatResp.Result, &chatResult); jsonErr == nil {
		chatTitle = chatResult.Title
		if chatTitle == "" {
			chatTitle = chatResult.FirstName
		}
	}
	return botName, chatTitle, nil
}

func retryAfterFromErr(err error) (time.Duration, bool) {
	if err == nil {
		return 0, false
	}
	msg := err.Error()
	const prefix = "[retry_after="
	idx := strings.Index(msg, prefix)
	if idx < 0 {
		return 0, false
	}
	rest := msg[idx+len(prefix):]
	end := strings.Index(rest, "]")
	if end < 0 {
		return 0, false
	}
	secs := 0
	for _, c := range rest[:end] {
		if c < '0' || c > '9' {
			return 0, false
		}
		secs = secs*10 + int(c-'0')
	}
	if secs <= 0 {
		return 0, false
	}
	return time.Duration(secs) * time.Second, true
}
