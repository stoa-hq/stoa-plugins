package stripe

import (
	"errors"
	"fmt"
)

// Config holds all configuration for the Stripe plugin.
// Keys are read from AppContext.Config:
//
//	stripe.secret_key        — Stripe secret key (sk_live_... or sk_test_...)
//	stripe.publishable_key   — Stripe publishable key (pk_live_... or pk_test_...)
//	stripe.webhook_secret    — Stripe webhook signing secret (whsec_...)
//	stripe.currency          — Default ISO 4217 currency code (default: EUR)
type Config struct {
	SecretKey      string
	PublishableKey string
	WebhookSecret  string
	Currency       string
}

func configFrom(raw map[string]interface{}) (Config, error) {
	cfg := Config{
		Currency: "EUR",
	}

	sub, ok := raw["stripe"]
	if !ok {
		return cfg, errors.New("missing config section \"stripe\"")
	}

	m, ok := sub.(map[string]interface{})
	if !ok {
		return cfg, errors.New("config section \"stripe\" must be a map")
	}

	if v, ok := m["secret_key"].(string); ok && v != "" {
		cfg.SecretKey = v
	} else {
		return cfg, fmt.Errorf("stripe.secret_key is required")
	}

	if v, ok := m["publishable_key"].(string); ok && v != "" {
		cfg.PublishableKey = v
	} else {
		return cfg, fmt.Errorf("stripe.publishable_key is required")
	}

	if v, ok := m["webhook_secret"].(string); ok && v != "" {
		cfg.WebhookSecret = v
	} else {
		return cfg, fmt.Errorf("stripe.webhook_secret is required")
	}

	if v, ok := m["currency"].(string); ok && v != "" {
		cfg.Currency = v
	}

	return cfg, nil
}
