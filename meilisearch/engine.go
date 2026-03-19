package meilisearch

import (
	"context"
	"fmt"
	"regexp"
	"time"

	ms "github.com/meilisearch/meilisearch-go"
	"github.com/rs/zerolog"
	"github.com/stoa-hq/stoa/pkg/sdk"
)

// validLocale matches BCP-47 locale tags like "de", "de-DE", "en-US".
var validLocale = regexp.MustCompile(`^[a-zA-Z]{2}(-[a-zA-Z]{2,8})?$`)

// allowedEntityTypes restricts which entity types can be searched.
var allowedEntityTypes = map[string]bool{
	"product":  true,
	"category": true,
}

// indexManager abstracts the Meilisearch index operations for testability.
type indexManager interface {
	Search(query string, request *ms.SearchRequest) (*ms.SearchResponse, error)
	AddDocuments(documents interface{}, primaryKey ...string) (*ms.TaskInfo, error)
	DeleteDocument(identifier string) (*ms.TaskInfo, error)
	UpdateSettings(settings *ms.Settings) (*ms.TaskInfo, error)
}

// clientProvider abstracts the Meilisearch client for testability.
type clientProvider interface {
	Index(uid string) indexManager
	WaitForTask(taskUID int64, interval time.Duration) (*ms.Task, error)
}

// meiliClientAdapter wraps the real meilisearch client to satisfy clientProvider.
type meiliClientAdapter struct {
	client ms.ServiceManager
}

func (a *meiliClientAdapter) Index(uid string) indexManager {
	return a.client.Index(uid)
}

func (a *meiliClientAdapter) WaitForTask(taskUID int64, interval time.Duration) (*ms.Task, error) {
	return a.client.WaitForTask(taskUID, interval)
}

// MeilisearchEngine implements sdk.SearchEngine using Meilisearch as the backend.
type MeilisearchEngine struct {
	client clientProvider
	cfg    Config
	logger zerolog.Logger
}

// NewMeilisearchEngine creates a new engine backed by the given Meilisearch client.
func NewMeilisearchEngine(client ms.ServiceManager, cfg Config, logger zerolog.Logger) *MeilisearchEngine {
	return &MeilisearchEngine{
		client: &meiliClientAdapter{client: client},
		cfg:    cfg,
		logger: logger,
	}
}

// newMeilisearchEngineWithProvider creates an engine with a custom client provider (for testing).
func newMeilisearchEngineWithProvider(client clientProvider, cfg Config, logger zerolog.Logger) *MeilisearchEngine {
	return &MeilisearchEngine{
		client: client,
		cfg:    cfg,
		logger: logger,
	}
}

func (e *MeilisearchEngine) productsIndex() string {
	return e.cfg.IndexPrefix + "_products"
}

func (e *MeilisearchEngine) categoriesIndex() string {
	return e.cfg.IndexPrefix + "_categories"
}

func (e *MeilisearchEngine) indexName(entityType string) string {
	// Handle irregular plurals.
	switch entityType {
	case "category":
		return e.categoriesIndex()
	default:
		return e.cfg.IndexPrefix + "_" + entityType + "s"
	}
}

func (e *MeilisearchEngine) Search(_ context.Context, req sdk.SearchRequest) (*sdk.SearchResponse, error) {
	if req.Page < 1 {
		req.Page = 1
	}
	if req.Limit < 1 || req.Limit > 100 {
		req.Limit = 25
	}

	offset := int64((req.Page - 1) * req.Limit)

	// Build filter conditions using array-based filters to prevent injection.
	var filters [][]string
	if req.Locale != "" && validLocale.MatchString(req.Locale) {
		filters = append(filters, []string{"locale = " + req.Locale})
	}

	msReq := &ms.SearchRequest{
		Limit:  int64(req.Limit),
		Offset: offset,
	}
	if len(filters) > 0 {
		msReq.Filter = filters
	}

	// Determine which indexes to search (only allowed entity types).
	var types []string
	if len(req.Types) == 0 {
		types = []string{"product", "category"}
	} else {
		for _, t := range req.Types {
			if allowedEntityTypes[t] {
				types = append(types, t)
			}
		}
		if len(types) == 0 {
			return &sdk.SearchResponse{Page: req.Page, Limit: req.Limit}, nil
		}
	}

	var allResults []sdk.SearchResult
	var totalHits int

	for _, typ := range types {
		indexName := e.indexName(typ)
		searchRes, err := e.client.Index(indexName).Search(req.Query, msReq)
		if err != nil {
			e.logger.Warn().Err(err).Str("index", indexName).Msg("search failed")
			continue
		}

		for _, hit := range searchRes.Hits {
			m, ok := hit.(map[string]interface{})
			if !ok {
				continue
			}
			result := sdk.SearchResult{
				Type: typ,
				Data: m,
			}
			if v, ok := m["entity_id"].(string); ok {
				result.ID = v
			}
			if v, ok := m["name"].(string); ok {
				result.Title = v
			}
			if v, ok := m["description"].(string); ok {
				result.Description = v
			}
			if v, ok := m["slug"].(string); ok {
				result.Slug = v
			}
			allResults = append(allResults, result)
		}
		totalHits += int(searchRes.EstimatedTotalHits)
	}

	return &sdk.SearchResponse{
		Results: allResults,
		Total:   totalHits,
		Page:    req.Page,
		Limit:   req.Limit,
	}, nil
}

func (e *MeilisearchEngine) Index(_ context.Context, entityType string, id string, data map[string]interface{}) error {
	indexName := e.indexName(entityType)
	doc := make(map[string]interface{}, len(data)+1)
	for k, v := range data {
		doc[k] = v
	}
	// Use the provided id as the Meilisearch document ID.
	doc["id"] = id

	_, err := e.client.Index(indexName).AddDocuments([]map[string]interface{}{doc}, "id")
	if err != nil {
		return fmt.Errorf("indexing %s/%s: %w", entityType, id, err)
	}
	return nil
}

func (e *MeilisearchEngine) Remove(_ context.Context, entityType string, id string) error {
	indexName := e.indexName(entityType)
	_, err := e.client.Index(indexName).DeleteDocument(id)
	if err != nil {
		return fmt.Errorf("removing %s/%s: %w", entityType, id, err)
	}
	return nil
}
