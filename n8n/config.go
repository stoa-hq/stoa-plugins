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
//	n8n.hooks             — optional list of after-hooks to forward (default: all)
type Config struct {
	// WebhookBaseURL is the n8n webhook base URL. Each event is dispatched to
	// {WebhookBaseURL}/{event_name}, e.g. .../webhook/stoa/order.after_create
	WebhookBaseURL string
	// Secret is used to sign payloads with HMAC-SHA256.
	// n8n can verify the X-Stoa-Signature header on the webhook node.
	Secret string
	// Timeout for outgoing HTTP calls to n8n.
	Timeout time.Duration
	// Hooks is an optional list of after-hook names to forward to n8n.
	// When nil, all after-hooks are forwarded (backward-compatible default).
	Hooks []string
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

	// Parse optional hooks filter.
	if rawHooks, ok := m["hooks"]; ok {
		list, ok := rawHooks.([]interface{})
		if !ok {
			return cfg, errors.New("n8n.hooks must be a list of strings")
		}
		hooks := make([]string, 0, len(list))
		for _, v := range list {
			s, ok := v.(string)
			if !ok {
				return cfg, fmt.Errorf("n8n.hooks entry must be a string, got %T", v)
			}
			if !validAfterHooks[s] {
				return cfg, fmt.Errorf("n8n.hooks: %q is not a valid after-hook", s)
			}
			hooks = append(hooks, s)
		}
		if len(hooks) > 0 {
			cfg.Hooks = hooks
		}
	}

	return cfg, nil
}
