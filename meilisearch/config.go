package meilisearch

import (
	"errors"
	"fmt"
)

// Config holds all configuration for the Meilisearch plugin.
// Keys are read from AppContext.Config:
//
//	meilisearch.host          — Meilisearch server URL (e.g. "http://localhost:7700")
//	meilisearch.api_key       — Meilisearch API key
//	meilisearch.index_prefix  — Prefix for index names (default: "stoa")
//	meilisearch.sync_on_start — Run full sync on startup (default: true)
//	meilisearch.batch_size    — Batch size for bulk indexing (default: 500)
type Config struct {
	Host        string
	APIKey      string
	IndexPrefix string
	SyncOnStart bool
	BatchSize   int
}

func configFrom(raw map[string]interface{}) (Config, error) {
	cfg := Config{
		IndexPrefix: "stoa",
		SyncOnStart: true,
		BatchSize:   500,
	}

	sub, ok := raw["meilisearch"]
	if !ok {
		return cfg, errors.New("missing config section \"meilisearch\"")
	}

	m, ok := sub.(map[string]interface{})
	if !ok {
		return cfg, errors.New("config section \"meilisearch\" must be a map")
	}

	if v, ok := m["host"].(string); ok && v != "" {
		cfg.Host = v
	} else {
		return cfg, fmt.Errorf("meilisearch.host is required")
	}

	if v, ok := m["api_key"].(string); ok && v != "" {
		cfg.APIKey = v
	} else {
		return cfg, fmt.Errorf("meilisearch.api_key is required")
	}

	if v, ok := m["index_prefix"].(string); ok && v != "" {
		cfg.IndexPrefix = v
	}

	if v, ok := m["sync_on_start"].(bool); ok {
		cfg.SyncOnStart = v
	}

	if v, ok := m["batch_size"].(int); ok && v > 0 {
		cfg.BatchSize = v
	}

	return cfg, nil
}
