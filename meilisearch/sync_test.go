package meilisearch

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stoa-hq/stoa/pkg/sdk"
)

func TestExtractEntityID_FromMetadata(t *testing.T) {
	id := uuid.New()
	event := &sdk.HookEvent{
		Name: "product.after_create",
		Metadata: map[string]interface{}{
			"id": id.String(),
		},
	}

	got := extractEntityID(event)
	if got != id {
		t.Errorf("extractEntityID() = %v, want %v", got, id)
	}
}

func TestExtractEntityID_FromMapEntity(t *testing.T) {
	id := uuid.New()
	event := &sdk.HookEvent{
		Name: "product.after_create",
		Entity: map[string]interface{}{
			"id": id.String(),
		},
	}

	got := extractEntityID(event)
	if got != id {
		t.Errorf("extractEntityID() = %v, want %v", got, id)
	}
}

func TestExtractEntityID_NilReturnsNil(t *testing.T) {
	event := &sdk.HookEvent{
		Name: "product.after_create",
	}

	got := extractEntityID(event)
	if got != uuid.Nil {
		t.Errorf("extractEntityID() = %v, want Nil", got)
	}
}

func TestExtractEntityID_InvalidUUID(t *testing.T) {
	event := &sdk.HookEvent{
		Name: "product.after_create",
		Metadata: map[string]interface{}{
			"id": "not-a-uuid",
		},
	}

	got := extractEntityID(event)
	if got != uuid.Nil {
		t.Errorf("extractEntityID() = %v, want Nil", got)
	}
}

func TestExtractEntityID_FromStructField(t *testing.T) {
	id := uuid.New()
	// Simulates an internal entity struct with a public ID field (e.g. category.Category).
	type fakeEntity struct {
		ID   uuid.UUID
		Name string
	}
	event := &sdk.HookEvent{
		Name:   "category.after_update",
		Entity: &fakeEntity{ID: id, Name: "Test"},
	}

	got := extractEntityID(event)
	if got != id {
		t.Errorf("extractEntityID() = %v, want %v", got, id)
	}
}

func TestExtractEntityID_FromStructFieldNonPointer(t *testing.T) {
	id := uuid.New()
	type fakeEntity struct {
		ID uuid.UUID
	}
	event := &sdk.HookEvent{
		Name:   "category.after_create",
		Entity: fakeEntity{ID: id},
	}

	got := extractEntityID(event)
	if got != id {
		t.Errorf("extractEntityID() = %v, want %v", got, id)
	}
}

func TestSyncer_RegisterHooks(t *testing.T) {
	hooks := sdk.NewHookRegistry()
	engine := newMeilisearchEngineWithProvider(newMockClient(), Config{IndexPrefix: "stoa"}, zerolog.Nop())
	syncer := NewSyncer(nil, engine, Config{}, zerolog.Nop())

	syncer.RegisterHooks(hooks)

	// Verify hooks are registered by dispatching events.
	// The handlers will fail (nil DB) but they shouldn't panic — they run in goroutines.
	hookNames := []string{
		sdk.HookAfterProductCreate,
		sdk.HookAfterProductUpdate,
		sdk.HookAfterProductDelete,
		sdk.HookAfterCategoryCreate,
		sdk.HookAfterCategoryUpdate,
		sdk.HookAfterCategoryDelete,
	}

	for _, name := range hookNames {
		err := hooks.Dispatch(context.Background(), &sdk.HookEvent{
			Name: name,
			Metadata: map[string]interface{}{
				"id": uuid.New().String(),
			},
		})
		if err != nil {
			t.Errorf("dispatch %q error = %v", name, err)
		}
	}
}

func TestNewSyncer(t *testing.T) {
	engine := newMeilisearchEngineWithProvider(newMockClient(), Config{IndexPrefix: "stoa"}, zerolog.Nop())
	cfg := Config{BatchSize: 100, IndexPrefix: "stoa"}

	syncer := NewSyncer(nil, engine, cfg, zerolog.Nop())
	if syncer == nil {
		t.Fatal("NewSyncer returned nil")
	}
	if syncer.engine != engine {
		t.Error("engine not set")
	}
	if syncer.cfg.BatchSize != 100 {
		t.Errorf("cfg.BatchSize = %d, want 100", syncer.cfg.BatchSize)
	}
}
