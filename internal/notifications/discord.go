package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	DiscordTimeout = 15 * time.Second
	DeliveryLease  = 60 * time.Second
	MaxRetryAfter  = 2 * time.Hour
)

var ErrInvalidDiscordWebhook = errors.New("invalid Discord webhook URL")

// ValidateDiscordWebhook enforces the provider-specific egress contract.
func ValidateDiscordWebhook(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Hostname() != "discord.com" {
		return ErrInvalidDiscordWebhook
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || strings.Contains(raw, "#") || parsed.Opaque != "" || parsed.RawPath != "" {
		return ErrInvalidDiscordWebhook
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return ErrInvalidDiscordWebhook
	}
	parts := strings.Split(parsed.Path, "/")
	if len(parts) != 5 || parts[0] != "" || parts[1] != "api" || parts[2] != "webhooks" || parts[3] == "" || parts[4] == "" {
		return ErrInvalidDiscordWebhook
	}
	return nil
}

type DiscordSender struct {
	Client  *http.Client
	Timeout time.Duration
}

func (sender DiscordSender) Send(ctx context.Context, claim DeliveryClaim) SendResult {
	if err := ValidateDiscordWebhook(claim.WebhookURL); err != nil {
		return SendResult{ErrorCode: "invalid_webhook", Err: err}
	}
	timeout := sender.Timeout
	if timeout == 0 {
		timeout = DiscordTimeout
	}
	client := sender.Client
	if client == nil {
		client = &http.Client{Timeout: timeout, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	} else {
		copy := *client
		copy.Timeout = timeout
		copy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
		client = &copy
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, claim.WebhookURL, bytes.NewReader(claim.Payload))
	if err != nil {
		return SendResult{ErrorCode: "request", Err: err}
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return SendResult{ErrorCode: "transport", Err: err}
	}
	defer response.Body.Close()
	var discard [1024]byte
	_, _ = response.Body.Read(discard[:])
	result := SendResult{StatusCode: response.StatusCode}
	if response.StatusCode >= 200 && response.StatusCode <= 299 {
		return result
	}
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		result.RetryAfter = parseRetryAfter(response.Header.Get("Retry-After"), time.Now().UTC())
		result.ErrorCode = "provider_retryable"
		return result
	}
	result.ErrorCode = fmt.Sprintf("provider_http_%d", response.StatusCode)
	return result
}

func parseRetryAfter(raw string, now time.Time) time.Duration {
	if raw == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && seconds >= 0 {
		if time.Duration(seconds) > MaxRetryAfter/time.Second {
			return MaxRetryAfter
		}
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(raw); err == nil {
		if value := when.Sub(now); value > 0 {
			if value > MaxRetryAfter {
				return MaxRetryAfter
			}
			return value
		}
	}
	return 0
}

func TestPayload(channelName string) []byte {
	return payload(map[string]any{"content": "Atalaya test notification for " + channelName})
}

func IncidentPayload(event, severity, ruleName, targetName string, value *float64, at time.Time) []byte {
	body := map[string]any{
		"content":  "Atalaya " + event + ": " + ruleName + " / " + targetName,
		"event":    event,
		"severity": severity,
		"rule":     ruleName,
		"target":   targetName,
		"at":       at.UTC().Format(time.RFC3339Nano),
	}
	if value != nil {
		body["value"] = *value
	}
	return payload(body)
}

func payload(value map[string]any) []byte {
	encoded, _ := json.Marshal(value)
	return encoded
}
