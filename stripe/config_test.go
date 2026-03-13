package stripe

import "testing"

func TestConfigFrom_Valid(t *testing.T) {
	raw := map[string]interface{}{
		"stripe": map[string]interface{}{
			"secret_key":      "sk_test_abc",
			"publishable_key": "pk_test_abc",
			"webhook_secret":  "whsec_abc",
		},
	}

	cfg, err := configFrom(raw)
	if err != nil {
		t.Fatalf("configFrom() error = %v", err)
	}
	if cfg.SecretKey != "sk_test_abc" {
		t.Errorf("SecretKey = %q, want %q", cfg.SecretKey, "sk_test_abc")
	}
	if cfg.Currency != "EUR" {
		t.Errorf("Currency = %q, want default %q", cfg.Currency, "EUR")
	}
}

func TestConfigFrom_CustomCurrency(t *testing.T) {
	raw := map[string]interface{}{
		"stripe": map[string]interface{}{
			"secret_key":      "sk_test_abc",
			"publishable_key": "pk_test_abc",
			"webhook_secret":  "whsec_abc",
			"currency":        "USD",
		},
	}

	cfg, err := configFrom(raw)
	if err != nil {
		t.Fatalf("configFrom() error = %v", err)
	}
	if cfg.Currency != "USD" {
		t.Errorf("Currency = %q, want %q", cfg.Currency, "USD")
	}
}

func TestConfigFrom_MissingSection(t *testing.T) {
	if _, err := configFrom(map[string]interface{}{}); err == nil {
		t.Error("expected error for missing stripe config section")
	}
}

func TestConfigFrom_MissingSecretKey(t *testing.T) {
	raw := map[string]interface{}{
		"stripe": map[string]interface{}{
			"publishable_key": "pk_test_abc",
			"webhook_secret":  "whsec_abc",
		},
	}
	if _, err := configFrom(raw); err == nil {
		t.Error("expected error for missing secret_key")
	}
}

func TestConfigFrom_MissingPublishableKey(t *testing.T) {
	raw := map[string]interface{}{
		"stripe": map[string]interface{}{
			"secret_key":     "sk_test_abc",
			"webhook_secret": "whsec_abc",
		},
	}
	if _, err := configFrom(raw); err == nil {
		t.Error("expected error for missing publishable_key")
	}
}

func TestConfigFrom_MissingWebhookSecret(t *testing.T) {
	raw := map[string]interface{}{
		"stripe": map[string]interface{}{
			"secret_key":      "sk_test_abc",
			"publishable_key": "pk_test_abc",
		},
	}
	if _, err := configFrom(raw); err == nil {
		t.Error("expected error for missing webhook_secret")
	}
}
