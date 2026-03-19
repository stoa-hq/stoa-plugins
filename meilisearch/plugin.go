// Package meilisearch provides a Stoa plugin that replaces the default
// PostgreSQL full-text search with Meilisearch.
//
// The plugin implements sdk.SearchPlugin, providing a pluggable search
// engine that integrates with Stoa's existing search endpoint
// (GET /api/v1/store/search).
//
// # Configuration (config.yaml)
//
//	plugins:
//	  meilisearch:
//	    host:          "http://localhost:7700"
//	    api_key:       "master-key"
//	    index_prefix:  "stoa"        # Prefix for Meilisearch index names
//	    sync_on_start: true          # Full sync on startup
//	    batch_size:    500           # Bulk indexing batch size
//
// # Features
//
//   - Replaces PostgreSQL full-text search with Meilisearch
//   - Automatic sync via entity hooks (product/category CRUD)
//   - Manual reindex endpoint: POST /api/v1/admin/meilisearch/reindex
//   - Multi-locale support (one document per entity per locale)
//   - Configurable index settings (searchable, filterable, sortable attributes)
package meilisearch

import (
	"context"

	ms "github.com/meilisearch/meilisearch-go"
	"github.com/rs/zerolog"
	"github.com/stoa-hq/stoa/pkg/sdk"
)

const (
	pluginName    = "meilisearch"
	pluginVersion = "0.1.0"
)

// Plugin integrates Meilisearch as a search engine for Stoa.
type Plugin struct {
	engine *MeilisearchEngine
	syncer *Syncer
	logger zerolog.Logger
}

// New returns a new Meilisearch Plugin ready to be registered.
func New() *Plugin { return &Plugin{} }

func init() { sdk.Register(New()) }

func (p *Plugin) Name() string        { return pluginName }
func (p *Plugin) Version() string     { return pluginVersion }
func (p *Plugin) Description() string { return "Meilisearch search engine for Stoa" }

// SearchEngine implements sdk.SearchPlugin.
func (p *Plugin) SearchEngine() sdk.SearchEngine { return p.engine }

// Init reads config, creates the Meilisearch client, registers hooks, and
// mounts the admin reindex route.
func (p *Plugin) Init(app *sdk.AppContext) error {
	p.logger = app.Logger.With().Str("plugin", pluginName).Logger()

	cfg, err := configFrom(app.Config)
	if err != nil {
		return err
	}

	client := ms.New(cfg.Host, ms.WithAPIKey(cfg.APIKey))
	p.engine = NewMeilisearchEngine(client, cfg, p.logger)
	p.syncer = NewSyncer(app.DB, p.engine, cfg, p.logger)
	p.syncer.RegisterHooks(app.Hooks)
	mountAdminRoutes(app.Router, p.syncer, app.Auth, p.logger)

	if cfg.SyncOnStart {
		go p.syncer.InitialSync(context.Background())
	}

	p.logger.Info().
		Str("host", cfg.Host).
		Str("prefix", cfg.IndexPrefix).
		Msg("meilisearch plugin initialised")

	return nil
}

// UIExtensions implements sdk.UIPlugin to provide a reindex button on the
// admin settings page.
func (p *Plugin) UIExtensions() []sdk.UIExtension {
	return []sdk.UIExtension{
		{
			ID:   "meilisearch_reindex",
			Slot: "admin:settings:plugins",
			Type: "schema",
			Schema: &sdk.UISchema{
				Title: map[string]string{
					"de-DE": "Meilisearch",
					"en-US": "Meilisearch",
				},
				Description: map[string]string{
					"de-DE": "Löst einen vollständigen Neuaufbau des Suchindex aus. Alle Produkte und Kategorien werden neu indiziert.",
					"en-US": "Triggers a full rebuild of the search index. All products and categories will be re-indexed.",
				},
				SubmitLabel: map[string]string{
					"de-DE": "Reindex starten",
					"en-US": "Start Reindex",
				},
				SuccessMessage: map[string]string{
					"de-DE": "Reindex wurde gestartet.",
					"en-US": "Reindex has been started.",
				},
				Fields:    []sdk.UISchemaField{},
				SubmitURL: "/admin/meilisearch/reindex",
			},
		},
	}
}

// Shutdown is a no-op; the Meilisearch client has no persistent connections.
func (p *Plugin) Shutdown() error { return nil }
