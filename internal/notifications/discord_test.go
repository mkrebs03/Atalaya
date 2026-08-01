package notifications

import (
	"strings"
	"testing"
	"time"
)

func TestValidateDiscordWebhookIsStrict(t *testing.T) {
	valid := "https://discord.com/api/webhooks/123456/token-value"
	if err := ValidateDiscordWebhook(valid); err != nil {
		t.Fatalf("valid webhook rejected: %v", err)
	}
	for _, raw := range []string{
		"http://discord.com/api/webhooks/123/token",
		"https://evil.example/api/webhooks/123/token",
		"https://discord.com:8443/api/webhooks/123/token",
		"https://user:pass@discord.com/api/webhooks/123/token",
		"https://discord.com/api/webhooks/123/token?next=evil",
		"https://discord.com/api/webhooks/123/token?",
		"https://discord.com/api/webhooks/123/token#",
		"https://discord.com/api/webhooks/123/token/extra",
		"https://discord.com/api/webhooks/123/token%2Fextra",
		"https://discord.com/api/webhooks//token",
	} {
		if err := ValidateDiscordWebhook(raw); err == nil {
			t.Fatalf("webhook accepted: %s", raw)
		}
	}
}

func TestIncidentPayloadDoesNotContainWebhook(t *testing.T) {
	payload := string(IncidentPayload("firing", "critical", "Host down", "Oracle", nil, testNotificationTime()))
	if strings.Contains(payload, "discord.com") || strings.Contains(payload, "webhook") {
		t.Fatalf("payload leaks destination: %s", payload)
	}
}

func TestParseRetryAfterIsBounded(t *testing.T) {
	if got := parseRetryAfter("9999999999", testNotificationTime()); got != MaxRetryAfter {
		t.Fatalf("parseRetryAfter()=%s, want %s", got, MaxRetryAfter)
	}
	if got := parseRetryAfter("0", testNotificationTime()); got != 0 {
		t.Fatalf("parseRetryAfter(0)=%s, want zero", got)
	}
}

func testNotificationTime() (value time.Time) {
	return time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
}
