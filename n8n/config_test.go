package n8n

import (
	"testing"
	"time"
)

func TestConfigFrom_Valid(t *testing.T) {
	raw := map[string]interface{}{
		"n8n": map[string]interface{}{
			"webhook_base_url": "http://n8n:5678/webhook/stoa",
			"secret":           "supersecret",
			"timeout_seconds":  float64(30),
		},
	}

	cfg, err := configFrom(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.WebhookBaseURL != "http://n8n:5678/webhook/stoa" {
		t.Errorf("WebhookBaseURL = %q", cfg.WebhookBaseURL)
	}
	if cfg.Secret != "supersecret" {
		t.Errorf("Secret = %q", cfg.Secret)
	}
	if cfg.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v", cfg.Timeout)
	}
}

func TestConfigFrom_DefaultTimeout(t *testing.T) {
	raw := map[string]interface{}{
		"n8n": map[string]interface{}{
			"webhook_base_url": "http://n8n:5678/webhook/stoa",
			"secret":           "s",
		},
	}

	cfg, err := configFrom(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Timeout != 10*time.Second {
		t.Errorf("expected default timeout 10s, got %v", cfg.Timeout)
	}
}

func TestConfigFrom_MissingSection(t *testing.T) {
	_, err := configFrom(map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error for missing n8n section")
	}
}

func TestConfigFrom_MissingWebhookURL(t *testing.T) {
	raw := map[string]interface{}{
		"n8n": map[string]interface{}{
			"secret": "s",
		},
	}
	_, err := configFrom(raw)
	if err == nil {
		t.Fatal("expected error for missing webhook_base_url")
	}
}

func TestConfigFrom_MissingSecret(t *testing.T) {
	raw := map[string]interface{}{
		"n8n": map[string]interface{}{
			"webhook_base_url": "http://n8n:5678/webhook/stoa",
		},
	}
	_, err := configFrom(raw)
	if err == nil {
		t.Fatal("expected error for missing secret")
	}
}

func TestConfigFrom_HooksSubset(t *testing.T) {
	raw := map[string]interface{}{
		"n8n": map[string]interface{}{
			"webhook_base_url": "http://n8n:5678/webhook/stoa",
			"secret":           "s",
			"hooks": []interface{}{
				"order.after_create",
				"payment.after_complete",
			},
		},
	}
	cfg, err := configFrom(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Hooks) != 2 {
		t.Fatalf("expected 2 hooks, got %d", len(cfg.Hooks))
	}
	if cfg.Hooks[0] != "order.after_create" || cfg.Hooks[1] != "payment.after_complete" {
		t.Errorf("unexpected hooks: %v", cfg.Hooks)
	}
}

func TestConfigFrom_HooksOmitted(t *testing.T) {
	raw := map[string]interface{}{
		"n8n": map[string]interface{}{
			"webhook_base_url": "http://n8n:5678/webhook/stoa",
			"secret":           "s",
		},
	}
	cfg, err := configFrom(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Hooks != nil {
		t.Errorf("expected nil hooks when omitted, got %v", cfg.Hooks)
	}
}

func TestConfigFrom_HooksInvalidName(t *testing.T) {
	raw := map[string]interface{}{
		"n8n": map[string]interface{}{
			"webhook_base_url": "http://n8n:5678/webhook/stoa",
			"secret":           "s",
			"hooks":            []interface{}{"order.after_craete"}, // typo
		},
	}
	_, err := configFrom(raw)
	if err == nil {
		t.Fatal("expected error for invalid hook name")
	}
}

func TestConfigFrom_HooksBeforeHook(t *testing.T) {
	raw := map[string]interface{}{
		"n8n": map[string]interface{}{
			"webhook_base_url": "http://n8n:5678/webhook/stoa",
			"secret":           "s",
			"hooks":            []interface{}{"order.before_create"},
		},
	}
	_, err := configFrom(raw)
	if err == nil {
		t.Fatal("expected error for before-hook in hooks list")
	}
}

func TestConfigFrom_HooksEmptyList(t *testing.T) {
	raw := map[string]interface{}{
		"n8n": map[string]interface{}{
			"webhook_base_url": "http://n8n:5678/webhook/stoa",
			"secret":           "s",
			"hooks":            []interface{}{},
		},
	}
	cfg, err := configFrom(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Hooks != nil {
		t.Errorf("expected nil hooks for empty list, got %v", cfg.Hooks)
	}
}
