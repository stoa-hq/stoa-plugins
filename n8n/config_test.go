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
