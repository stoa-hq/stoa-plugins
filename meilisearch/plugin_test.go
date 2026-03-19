package meilisearch

import (
	"testing"

	"github.com/stoa-hq/stoa/pkg/sdk"
)

func TestPlugin_Metadata(t *testing.T) {
	p := New()

	if p.Name() != "meilisearch" {
		t.Errorf("Name() = %q, want %q", p.Name(), "meilisearch")
	}
	if p.Version() == "" {
		t.Error("Version() must not be empty")
	}
	if p.Description() == "" {
		t.Error("Description() must not be empty")
	}
}

func TestPlugin_Shutdown(t *testing.T) {
	p := New()
	if err := p.Shutdown(); err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}
}

func TestPlugin_ImplementsSearchPlugin(t *testing.T) {
	p := New()
	var _ sdk.SearchPlugin = p
	var _ sdk.Plugin = p
}

func TestPlugin_ImplementsUIPlugin(t *testing.T) {
	var _ sdk.UIPlugin = (*Plugin)(nil)
}

func TestPlugin_UIExtensions(t *testing.T) {
	p := New()
	exts := p.UIExtensions()

	if len(exts) != 1 {
		t.Fatalf("UIExtensions() returned %d extensions, want 1", len(exts))
	}

	ext := exts[0]
	if ext.ID != "meilisearch_reindex" {
		t.Errorf("ID = %q, want %q", ext.ID, "meilisearch_reindex")
	}
	if ext.Slot != "admin:settings:plugins" {
		t.Errorf("Slot = %q, want %q", ext.Slot, "admin:settings:plugins")
	}
	if ext.Type != "schema" {
		t.Errorf("Type = %q, want %q", ext.Type, "schema")
	}
	if ext.Schema == nil {
		t.Fatal("Schema is nil")
	}
	if ext.Schema.SubmitURL != "/admin/meilisearch/reindex" {
		t.Errorf("SubmitURL = %q, want %q", ext.Schema.SubmitURL, "/admin/meilisearch/reindex")
	}
	if len(ext.Schema.Fields) != 0 {
		t.Errorf("Fields should be empty, got %d", len(ext.Schema.Fields))
	}
	if ext.Schema.Title["en-US"] != "Meilisearch" {
		t.Errorf("Title[en-US] = %q, want %q", ext.Schema.Title["en-US"], "Meilisearch")
	}
	if ext.Schema.SubmitLabel["en-US"] != "Start Reindex" {
		t.Errorf("SubmitLabel[en-US] = %q, want %q", ext.Schema.SubmitLabel["en-US"], "Start Reindex")
	}

	if err := sdk.ValidateUIExtension(p.Name(), ext); err != nil {
		t.Errorf("ValidateUIExtension() failed: %v", err)
	}
}

func TestPlugin_SearchEngine_NilBeforeInit(t *testing.T) {
	p := New()
	// Before Init, engine field is nil. SearchEngine() returns the nil
	// *MeilisearchEngine which, as a concrete type, is not == nil when
	// assigned to an interface. The important thing is that the underlying
	// engine pointer is nil.
	if p.engine != nil {
		t.Error("engine field should be nil before Init")
	}
}

func TestPlugin_Init_MissingConfig(t *testing.T) {
	// configFrom is the first thing Init calls.
	if _, err := configFrom(map[string]interface{}{}); err == nil {
		t.Error("expected error when meilisearch config section is absent")
	}
}
