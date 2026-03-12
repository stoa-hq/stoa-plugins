package n8n

import (
	"errors"
	"fmt"
	"time"
)

// Config holds all configuration for the n8n plugin.
// Keys are read from AppContext.Config:
//
//	n8n.webhook_base_url  — base URL of the n8n webhook (e.g. http://n8n:5678/webhook/stoa)
//	n8n.secret            — HMAC-SHA256 signing secret shared with n8n
//	n8n.timeout_seconds   — HTTP client timeout in seconds (default: 10)
type Config struct {
	// WebhookBaseURL is the n8n webhook base URL. Each event is dispatched to
	// {WebhookBaseURL}/{event_name}, e.g. .../webhook/stoa/order.after_create
	WebhookBaseURL string
	// Secret is used to sign payloads with HMAC-SHA256.
	// n8n can verify the X-Stoa-Signature header on the webhook node.
	Secret string
	// Timeout for outgoing HTTP calls to n8n.
	Timeout time.Duration
}

func configFrom(raw map[string]interface{}) (Config, error) {
	cfg := Config{
		Timeout: 10 * time.Second,
	}

	sub, ok := raw["n8n"]
	if !ok {
		return cfg, errors.New("missing config section \"n8n\"")
	}

	m, ok := sub.(map[string]interface{})
	if !ok {
		return cfg, errors.New("config section \"n8n\" must be a map")
	}

	if v, ok := m["webhook_base_url"].(string); ok && v != "" {
		cfg.WebhookBaseURL = v
	} else {
		return cfg, errors.New("n8n.webhook_base_url is required")
	}

	if v, ok := m["secret"].(string); ok {
		cfg.Secret = v
	}

	if v, ok := m["timeout_seconds"].(int); ok && v > 0 {
		cfg.Timeout = time.Duration(v) * time.Second
	} else if v, ok := m["timeout_seconds"].(float64); ok && v > 0 {
		cfg.Timeout = time.Duration(v) * time.Second
	}

	if cfg.Secret == "" {
		return cfg, fmt.Errorf("n8n.secret is required")
	}

	return cfg, nil
}
