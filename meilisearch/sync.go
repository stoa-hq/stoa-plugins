package meilisearch

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/stoa-hq/stoa/pkg/sdk"
)

const syncTimeout = 30 * time.Second

// Syncer handles data synchronization between PostgreSQL and Meilisearch.
type Syncer struct {
	db     *pgxpool.Pool
	engine *MeilisearchEngine
	cfg    Config
	logger zerolog.Logger
}

// NewSyncer creates a new syncer instance.
func NewSyncer(db *pgxpool.Pool, engine *MeilisearchEngine, cfg Config, logger zerolog.Logger) *Syncer {
	return &Syncer{
		db:     db,
		engine: engine,
		cfg:    cfg,
		logger: logger,
	}
}

// RegisterHooks registers lifecycle hooks for automatic index synchronization.
func (s *Syncer) RegisterHooks(hooks *sdk.HookRegistry) {
	hooks.On(sdk.HookAfterProductCreate, s.onProductChange)
	hooks.On(sdk.HookAfterProductUpdate, s.onProductChange)
	hooks.On(sdk.HookAfterProductDelete, s.onProductDelete)
	hooks.On(sdk.HookAfterCategoryCreate, s.onCategoryChange)
	hooks.On(sdk.HookAfterCategoryUpdate, s.onCategoryChange)
	hooks.On(sdk.HookAfterCategoryDelete, s.onCategoryDelete)
}

// onProductChange handles product create/update events.
func (s *Syncer) onProductChange(_ context.Context, event *sdk.HookEvent) error {
	id := extractEntityID(event)
	if id == uuid.Nil {
		s.logger.Warn().Str("hook", event.Name).Msg("could not extract entity ID")
		return nil
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), syncTimeout)
		defer cancel()

		if err := s.indexProduct(ctx, id); err != nil {
			s.logger.Error().Err(err).Str("product_id", id.String()).Msg("failed to index product")
		}
	}()

	return nil
}

// onProductDelete handles product delete events.
func (s *Syncer) onProductDelete(_ context.Context, event *sdk.HookEvent) error {
	id := extractEntityID(event)
	if id == uuid.Nil {
		s.logger.Warn().Str("hook", event.Name).Msg("could not extract entity ID")
		return nil
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), syncTimeout)
		defer cancel()

		if err := s.removeProduct(ctx, id); err != nil {
			s.logger.Error().Err(err).Str("product_id", id.String()).Msg("failed to remove product from index")
		}
	}()

	return nil
}

// onCategoryChange handles category create/update events.
func (s *Syncer) onCategoryChange(_ context.Context, event *sdk.HookEvent) error {
	id := extractEntityID(event)
	if id == uuid.Nil {
		s.logger.Warn().Str("hook", event.Name).Msg("could not extract entity ID")
		return nil
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), syncTimeout)
		defer cancel()

		if err := s.indexCategory(ctx, id); err != nil {
			s.logger.Error().Err(err).Str("category_id", id.String()).Msg("failed to index category")
		}
	}()

	return nil
}

// onCategoryDelete handles category delete events.
func (s *Syncer) onCategoryDelete(_ context.Context, event *sdk.HookEvent) error {
	id := extractEntityID(event)
	if id == uuid.Nil {
		s.logger.Warn().Str("hook", event.Name).Msg("could not extract entity ID")
		return nil
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), syncTimeout)
		defer cancel()

		if err := s.removeCategory(ctx, id); err != nil {
			s.logger.Error().Err(err).Str("category_id", id.String()).Msg("failed to remove category from index")
		}
	}()

	return nil
}

// indexProduct reads a product and its translations from DB and indexes all locale variants.
func (s *Syncer) indexProduct(ctx context.Context, productID uuid.UUID) error {
	rows, err := s.db.Query(ctx, `
		SELECT p.id, p.sku, p.active, p.price_net, p.price_gross, p.currency, p.created_at,
		       pt.locale, pt.name, pt.description, pt.slug
		FROM products p
		JOIN product_translations pt ON p.id = pt.product_id
		WHERE p.id = $1`, productID)
	if err != nil {
		return fmt.Errorf("querying product: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id                                   uuid.UUID
			sku                                  string
			active                               bool
			priceNet, priceGross                 int
			currency                             string
			createdAt                            time.Time
			locale, name, description, slug      string
		)
		if err := rows.Scan(&id, &sku, &active, &priceNet, &priceGross, &currency, &createdAt,
			&locale, &name, &description, &slug); err != nil {
			return fmt.Errorf("scanning product row: %w", err)
		}

		docID := id.String() + "_" + locale
		data := map[string]interface{}{
			"entity_id":   id.String(),
			"sku":         sku,
			"active":      active,
			"price_net":   priceNet,
			"price_gross": priceGross,
			"currency":    currency,
			"locale":      locale,
			"name":        name,
			"description": description,
			"slug":        slug,
			"created_at":  createdAt.Unix(),
		}

		if err := s.engine.Index(ctx, "product", docID, data); err != nil {
			return fmt.Errorf("indexing product %s locale %s: %w", id, locale, err)
		}
	}

	return rows.Err()
}

// removeProduct removes all locale variants of a product from the index.
func (s *Syncer) removeProduct(ctx context.Context, productID uuid.UUID) error {
	rows, err := s.db.Query(ctx,
		`SELECT locale FROM product_translations WHERE product_id = $1`, productID)
	if err != nil {
		return fmt.Errorf("querying product translations: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var locale string
		if err := rows.Scan(&locale); err != nil {
			return fmt.Errorf("scanning locale: %w", err)
		}
		docID := productID.String() + "_" + locale
		if err := s.engine.Remove(ctx, "product", docID); err != nil {
			s.logger.Warn().Err(err).Str("doc_id", docID).Msg("failed to remove product doc")
		}
	}
	return rows.Err()
}

// indexCategory reads a category and its translations from DB and indexes all locale variants.
func (s *Syncer) indexCategory(ctx context.Context, categoryID uuid.UUID) error {
	rows, err := s.db.Query(ctx, `
		SELECT c.id, c.parent_id, c.position, c.active, c.created_at,
		       ct.locale, ct.name, ct.description, ct.slug
		FROM categories c
		JOIN category_translations ct ON c.id = ct.category_id
		WHERE c.id = $1`, categoryID)
	if err != nil {
		return fmt.Errorf("querying category: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id                               uuid.UUID
			parentID                         *uuid.UUID
			position                         int
			active                           bool
			createdAt                        time.Time
			locale, name, description, slug  string
		)
		if err := rows.Scan(&id, &parentID, &position, &active, &createdAt,
			&locale, &name, &description, &slug); err != nil {
			return fmt.Errorf("scanning category row: %w", err)
		}

		docID := id.String() + "_" + locale
		data := map[string]interface{}{
			"entity_id":   id.String(),
			"active":      active,
			"position":    position,
			"locale":      locale,
			"name":        name,
			"description": description,
			"slug":        slug,
			"created_at":  createdAt.Unix(),
		}
		if parentID != nil {
			data["parent_id"] = parentID.String()
		}

		if err := s.engine.Index(ctx, "category", docID, data); err != nil {
			return fmt.Errorf("indexing category %s locale %s: %w", id, locale, err)
		}
	}

	return rows.Err()
}

// removeCategory removes all locale variants of a category from the index.
func (s *Syncer) removeCategory(ctx context.Context, categoryID uuid.UUID) error {
	rows, err := s.db.Query(ctx,
		`SELECT locale FROM category_translations WHERE category_id = $1`, categoryID)
	if err != nil {
		return fmt.Errorf("querying category translations: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var locale string
		if err := rows.Scan(&locale); err != nil {
			return fmt.Errorf("scanning locale: %w", err)
		}
		docID := categoryID.String() + "_" + locale
		if err := s.engine.Remove(ctx, "category", docID); err != nil {
			s.logger.Warn().Err(err).Str("doc_id", docID).Msg("failed to remove category doc")
		}
	}
	return rows.Err()
}

// InitialSync performs a full synchronization of all products and categories.
func (s *Syncer) InitialSync(ctx context.Context) {
	s.logger.Info().Msg("starting initial sync")
	start := time.Now()

	if err := s.FullReindex(ctx); err != nil {
		s.logger.Error().Err(err).Msg("initial sync failed")
		return
	}

	s.logger.Info().Dur("duration", time.Since(start)).Msg("initial sync completed")
}

// FullReindex configures index settings and re-indexes all products and categories.
func (s *Syncer) FullReindex(ctx context.Context) error {
	if s.db == nil {
		return fmt.Errorf("database connection not available")
	}

	// Step 1: Configure index settings.
	if err := configureIndexSettings(s.engine.client, s.cfg.IndexPrefix); err != nil {
		return fmt.Errorf("configuring index settings: %w", err)
	}

	// Step 2: Index all products.
	if err := s.reindexProducts(ctx); err != nil {
		return fmt.Errorf("reindexing products: %w", err)
	}

	// Step 3: Index all categories.
	if err := s.reindexCategories(ctx); err != nil {
		return fmt.Errorf("reindexing categories: %w", err)
	}

	return nil
}

func (s *Syncer) reindexProducts(ctx context.Context) error {
	rows, err := s.db.Query(ctx, `
		SELECT p.id, p.sku, p.active, p.price_net, p.price_gross, p.currency, p.created_at,
		       pt.locale, pt.name, pt.description, pt.slug
		FROM products p
		JOIN product_translations pt ON p.id = pt.product_id
		ORDER BY p.id`)
	if err != nil {
		return fmt.Errorf("querying products: %w", err)
	}
	defer rows.Close()

	var batch []map[string]interface{}
	indexed := 0

	for rows.Next() {
		var (
			id                                   uuid.UUID
			sku                                  string
			active                               bool
			priceNet, priceGross                 int
			currency                             string
			createdAt                            time.Time
			locale, name, description, slug      string
		)
		if err := rows.Scan(&id, &sku, &active, &priceNet, &priceGross, &currency, &createdAt,
			&locale, &name, &description, &slug); err != nil {
			return fmt.Errorf("scanning product: %w", err)
		}

		doc := map[string]interface{}{
			"id":           id.String() + "_" + locale,
			"entity_id":    id.String(),
			"sku":          sku,
			"active":       active,
			"price_net":    priceNet,
			"price_gross":  priceGross,
			"currency":     currency,
			"locale":       locale,
			"name":         name,
			"description":  description,
			"slug":         slug,
			"created_at":   createdAt.Unix(),
		}
		batch = append(batch, doc)

		if len(batch) >= s.cfg.BatchSize {
			taskInfo, err := s.engine.client.Index(s.engine.productsIndex()).AddDocuments(batch, "id")
			if err != nil {
				return fmt.Errorf("batch indexing products: %w", err)
			}
			if err := awaitTask(s.engine.client, taskInfo.TaskUID); err != nil {
				return fmt.Errorf("product batch task: %w", err)
			}
			indexed += len(batch)
			batch = batch[:0]
		}
	}

	if len(batch) > 0 {
		taskInfo, err := s.engine.client.Index(s.engine.productsIndex()).AddDocuments(batch, "id")
		if err != nil {
			return fmt.Errorf("batch indexing products: %w", err)
		}
		if err := awaitTask(s.engine.client, taskInfo.TaskUID); err != nil {
			return fmt.Errorf("product batch task: %w", err)
		}
		indexed += len(batch)
	}

	if indexed == 0 {
		s.logger.Warn().Msg("0 products found — check if products exist and have translations")
	} else {
		s.logger.Info().Int("count", indexed).Msg("products indexed")
	}
	return rows.Err()
}

func (s *Syncer) reindexCategories(ctx context.Context) error {
	rows, err := s.db.Query(ctx, `
		SELECT c.id, c.parent_id, c.position, c.active, c.created_at,
		       ct.locale, ct.name, ct.description, ct.slug
		FROM categories c
		JOIN category_translations ct ON c.id = ct.category_id
		ORDER BY c.id`)
	if err != nil {
		return fmt.Errorf("querying categories: %w", err)
	}
	defer rows.Close()

	var batch []map[string]interface{}
	indexed := 0

	for rows.Next() {
		var (
			id                               uuid.UUID
			parentID                         *uuid.UUID
			position                         int
			active                           bool
			createdAt                        time.Time
			locale, name, description, slug  string
		)
		if err := rows.Scan(&id, &parentID, &position, &active, &createdAt,
			&locale, &name, &description, &slug); err != nil {
			return fmt.Errorf("scanning category: %w", err)
		}

		doc := map[string]interface{}{
			"id":          id.String() + "_" + locale,
			"entity_id":   id.String(),
			"active":      active,
			"position":    position,
			"locale":      locale,
			"name":        name,
			"description": description,
			"slug":        slug,
			"created_at":  createdAt.Unix(),
		}
		if parentID != nil {
			doc["parent_id"] = parentID.String()
		}

		batch = append(batch, doc)

		if len(batch) >= s.cfg.BatchSize {
			taskInfo, err := s.engine.client.Index(s.engine.categoriesIndex()).AddDocuments(batch, "id")
			if err != nil {
				return fmt.Errorf("batch indexing categories: %w", err)
			}
			if err := awaitTask(s.engine.client, taskInfo.TaskUID); err != nil {
				return fmt.Errorf("category batch task: %w", err)
			}
			indexed += len(batch)
			batch = batch[:0]
		}
	}

	if len(batch) > 0 {
		taskInfo, err := s.engine.client.Index(s.engine.categoriesIndex()).AddDocuments(batch, "id")
		if err != nil {
			return fmt.Errorf("batch indexing categories: %w", err)
		}
		if err := awaitTask(s.engine.client, taskInfo.TaskUID); err != nil {
			return fmt.Errorf("category batch task: %w", err)
		}
		indexed += len(batch)
	}

	if indexed == 0 {
		s.logger.Warn().Msg("0 categories found — check if categories exist and have translations")
	} else {
		s.logger.Info().Int("count", indexed).Msg("categories indexed")
	}
	return rows.Err()
}

// extractEntityID attempts to extract a UUID from the hook event entity.
func extractEntityID(event *sdk.HookEvent) uuid.UUID {
	// Try metadata first.
	if event.Metadata != nil {
		if idStr, ok := event.Metadata["id"].(string); ok {
			if id, err := uuid.Parse(idStr); err == nil {
				return id
			}
		}
	}

	// Try the entity as an interface with GetID().
	type hasID interface {
		GetID() uuid.UUID
	}
	if e, ok := event.Entity.(hasID); ok {
		return e.GetID()
	}

	// Try the entity as a map.
	if m, ok := event.Entity.(map[string]interface{}); ok {
		if idStr, ok := m["id"].(string); ok {
			if id, err := uuid.Parse(idStr); err == nil {
				return id
			}
		}
	}

	// Use reflection to read a public ID field of type uuid.UUID.
	if event.Entity != nil {
		v := reflect.ValueOf(event.Entity)
		if v.Kind() == reflect.Ptr {
			v = v.Elem()
		}
		if v.Kind() == reflect.Struct {
			f := v.FieldByName("ID")
			if f.IsValid() {
				if id, ok := f.Interface().(uuid.UUID); ok {
					return id
				}
			}
		}
	}

	return uuid.Nil
}
