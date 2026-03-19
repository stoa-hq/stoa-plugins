package meilisearch

import "testing"

func TestConfigFrom_Valid(t *testing.T) {
	raw := map[string]interface{}{
		"meilisearch": map[string]interface{}{
			"host":    "http://localhost:7700",
			"api_key": "master-key",
		},
	}

	cfg, err := configFrom(raw)
	if err != nil {
		t.Fatalf("configFrom() error = %v", err)
	}
	if cfg.Host != "http://localhost:7700" {
		t.Errorf("Host = %q, want %q", cfg.Host, "http://localhost:7700")
	}
	if cfg.APIKey != "master-key" {
		t.Errorf("APIKey = %q, want %q", cfg.APIKey, "master-key")
	}
	if cfg.IndexPrefix != "stoa" {
		t.Errorf("IndexPrefix = %q, want default %q", cfg.IndexPrefix, "stoa")
	}
	if !cfg.SyncOnStart {
		t.Error("SyncOnStart should default to true")
	}
	if cfg.BatchSize != 500 {
		t.Errorf("BatchSize = %d, want default %d", cfg.BatchSize, 500)
	}
}

func TestConfigFrom_AllOptions(t *testing.T) {
	raw := map[string]interface{}{
		"meilisearch": map[string]interface{}{
			"host":          "http://meili:7700",
			"api_key":       "my-key",
			"index_prefix":  "shop",
			"sync_on_start": false,
			"batch_size":    100,
		},
	}

	cfg, err := configFrom(raw)
	if err != nil {
		t.Fatalf("configFrom() error = %v", err)
	}
	if cfg.IndexPrefix != "shop" {
		t.Errorf("IndexPrefix = %q, want %q", cfg.IndexPrefix, "shop")
	}
	if cfg.SyncOnStart {
		t.Error("SyncOnStart should be false")
	}
	if cfg.BatchSize != 100 {
		t.Errorf("BatchSize = %d, want %d", cfg.BatchSize, 100)
	}
}

func TestConfigFrom_MissingSection(t *testing.T) {
	if _, err := configFrom(map[string]interface{}{}); err == nil {
		t.Error("expected error for missing meilisearch config section")
	}
}

func TestConfigFrom_InvalidSection(t *testing.T) {
	raw := map[string]interface{}{
		"meilisearch": "not a map",
	}
	if _, err := configFrom(raw); err == nil {
		t.Error("expected error for non-map config section")
	}
}

func TestConfigFrom_MissingHost(t *testing.T) {
	raw := map[string]interface{}{
		"meilisearch": map[string]interface{}{
			"api_key": "key",
		},
	}
	if _, err := configFrom(raw); err == nil {
		t.Error("expected error for missing host")
	}
}

func TestConfigFrom_MissingAPIKey(t *testing.T) {
	raw := map[string]interface{}{
		"meilisearch": map[string]interface{}{
			"host": "http://localhost:7700",
		},
	}
	if _, err := configFrom(raw); err == nil {
		t.Error("expected error for missing api_key")
	}
}

func TestConfigFrom_EmptyHost(t *testing.T) {
	raw := map[string]interface{}{
		"meilisearch": map[string]interface{}{
			"host":    "",
			"api_key": "key",
		},
	}
	if _, err := configFrom(raw); err == nil {
		t.Error("expected error for empty host")
	}
}
