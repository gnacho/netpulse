package telegram

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
)

func TestFormatMessageUrgent(t *testing.T) {
	ev := alerts.AlertEvent{
		Category:    "router",
		Urgent:      true,
		Title:       "Agent down",
		Description: "The agent stopped responding",
		Hint:        "Check power and network",
		RouterID:    "rt1",
		Ts:          time.Date(2026, 8, 28, 21, 30, 0, 0, time.UTC).Unix(),
	}
	msg := formatMessage(ev)

	if !strings.Contains(msg, "🔴") {
		t.Error("urgent alert should contain red circle emoji")
	}
	if !strings.Contains(msg, "📡") {
		t.Error("router category should contain antenna emoji")
	}
	if !strings.Contains(msg, "<b>Agent down</b>") {
		t.Error("title should be bold")
	}
	if !strings.Contains(msg, "The agent stopped responding") {
		t.Error("description should be present")
	}
	if !strings.Contains(msg, "rt1") {
		t.Error("router ID should be present")
	}
	if !strings.Contains(msg, "<i>Check power and network</i>") {
		t.Error("hint should be italic")
	}
	if !strings.Contains(msg, "21:30:00") {
		// Timestamp is formatted in local timezone
		localTime := time.Date(2026, 8, 28, 21, 30, 0, 0, time.UTC).Local().Format("15:04:05")
		if !strings.Contains(msg, localTime) {
			t.Errorf("timestamp should be present, got msg: %s", msg)
		}
	}
}

func TestFormatMessageNonUrgent(t *testing.T) {
	ev := alerts.AlertEvent{
		Category: "system",
		Urgent:   false,
		Title:    "CPU high",
		Ts:       time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC).Unix(),
	}
	msg := formatMessage(ev)

	if !strings.Contains(msg, "⚠️") {
		t.Error("non-urgent alert should contain warning emoji")
	}
	if strings.Contains(msg, "🔴") {
		t.Error("non-urgent alert should NOT contain red circle")
	}
}

func TestFormatMessageHTMLEscape(t *testing.T) {
	ev := alerts.AlertEvent{
		Category:    "internet",
		Title:       "WAN <down>",
		Description: "Loss & latency > 100ms",
		Ts:          time.Now().Unix(),
	}
	msg := formatMessage(ev)

	if strings.Contains(msg, "<down>") {
		t.Error("angle brackets in title should be escaped")
	}
	if !strings.Contains(msg, "&lt;down&gt;") {
		t.Error("title should contain escaped HTML entities")
	}
	if strings.Contains(msg, "> 100ms") {
		t.Error("angle bracket in description should be escaped")
	}
}

func TestFormatMessageTruncatesLongDescription(t *testing.T) {
	ev := alerts.AlertEvent{
		Category:    "clients",
		Title:       "Test",
		Description: strings.Repeat("x", 600),
		Ts:          time.Now().Unix(),
	}
	msg := formatMessage(ev)

	if strings.Count(msg, "x") > 510 {
		t.Error("long description should be truncated to ~500 chars")
	}
}

func TestCategoryEmoji(t *testing.T) {
	cases := map[string]string{
		"router":   "📡",
		"internet": "🌐",
		"clients":  "📱",
		"signal":   "📶",
		"vpn":      "🔒",
		"system":   "⚙️",
		"unknown":  "🔔",
	}
	for cat, want := range cases {
		got := categoryEmoji(cat)
		if got != want {
			t.Errorf("categoryEmoji(%q) = %q, want %q", cat, got, want)
		}
	}
}

func TestHTMLEscape(t *testing.T) {
	cases := map[string]string{
		"hello":          "hello",
		"<b>bold</b>":    "&lt;b&gt;bold&lt;/b&gt;",
		"a & b":          "a &amp; b",
		"no special":     "no special",
		"<>&":            "&lt;&gt;&amp;",
	}
	for input, want := range cases {
		got := htmlEscape(input)
		if got != want {
			t.Errorf("htmlEscape(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestIsRetryable(t *testing.T) {
	cases := []struct {
		err  string
		want bool
	}{
		{"api error 400: bad request", false},
		{"api error 401: unauthorized", false},
		{"api error 403: forbidden", false},
		{"api error 404: not found", false},
		{"api error 429: too many requests", true},
		{"api error 500: internal", true},
		{"http do: connection refused", true},
		{"", false},
	}
	for _, tc := range cases {
		var err error
		if tc.err != "" {
			err = fmt.Errorf("%s", tc.err)
		}
		got := isRetryable(err)
		if got != tc.want {
			t.Errorf("isRetryable(%q) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

// mockKV implements kvStore for testing.
type mockKV struct {
	data map[string]string
}

func (m *mockKV) Get(key string) (string, bool) {
	v, ok := m.data[key]
	return v, ok
}

func (m *mockKV) Set(key, value string) error {
	m.data[key] = value
	return nil
}

func TestLoadSaveConfig(t *testing.T) {
	kv := &mockKV{data: make(map[string]string)}

	// Empty config
	cfg := LoadConfig(kv)
	if cfg.BotToken != "" || cfg.ChatID != "" || cfg.Enabled {
		t.Error("empty kv should return zero config")
	}

	// Save and reload
	want := Config{BotToken: "123:ABC", ChatID: "-100123456", Enabled: true}
	if err := SaveConfig(kv, want); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	got := LoadConfig(kv)
	if got.BotToken != want.BotToken {
		t.Errorf("BotToken = %q, want %q", got.BotToken, want.BotToken)
	}
	if got.ChatID != want.ChatID {
		t.Errorf("ChatID = %q, want %q", got.ChatID, want.ChatID)
	}
	if !got.Enabled {
		t.Error("Enabled should be true")
	}
}

func TestNotifyDropsWhenFull(t *testing.T) {
	kv := &mockKV{data: make(map[string]string)}
	n := &Notifier{
		kv:    kv,
		queue: make(chan alerts.AlertEvent, 2),
		done:  make(chan struct{}),
	}
	defer close(n.done)

	// Fill the queue
	n.Notify(alerts.AlertEvent{Title: "1"})
	n.Notify(alerts.AlertEvent{Title: "2"})
	// Third should be dropped (non-blocking)
	n.Notify(alerts.AlertEvent{Title: "3"})

	if len(n.queue) != 2 {
		t.Errorf("queue len = %d, want 2 (third should be dropped)", len(n.queue))
	}
}

func TestRetryAfterFromErr(t *testing.T) {
	cases := []struct {
		err      string
		wantSecs int
		wantOK   bool
	}{
		{"api error 429: too many requests [retry_after=30]", 30, true},
		{"api error 429: too many requests [retry_after=5]", 5, true},
		{"api error 400: bad request", 0, false},
		{"connection refused", 0, false},
		{"", 0, false},
	}
	for _, tc := range cases {
		var err error
		if tc.err != "" {
			err = fmt.Errorf("%s", tc.err)
		}
		dur, ok := retryAfterFromErr(err)
		if ok != tc.wantOK {
			t.Errorf("retryAfterFromErr(%q) ok = %v, want %v", tc.err, ok, tc.wantOK)
		}
		if ok && dur != time.Duration(tc.wantSecs)*time.Second {
			t.Errorf("retryAfterFromErr(%q) dur = %v, want %ds", tc.err, dur, tc.wantSecs)
		}
	}
}
