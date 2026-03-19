package meilisearch

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/stoa-hq/stoa/pkg/sdk"
)

func TestReindexHandler_ReturnsAccepted(t *testing.T) {
	engine := newMeilisearchEngineWithProvider(newMockClient(), Config{IndexPrefix: "stoa"}, zerolog.Nop())
	syncer := NewSyncer(nil, engine, Config{BatchSize: 500, IndexPrefix: "stoa"}, zerolog.Nop())

	handler := reindexHandler(syncer, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/meilisearch/reindex", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("status = %d, want %d", w.Code, http.StatusAccepted)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
}

func TestMountAdminRoutes(t *testing.T) {
	engine := newMeilisearchEngineWithProvider(newMockClient(), Config{IndexPrefix: "stoa"}, zerolog.Nop())
	syncer := NewSyncer(nil, engine, Config{BatchSize: 500, IndexPrefix: "stoa"}, zerolog.Nop())
	logger := zerolog.Nop()

	authMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
		})
	}
	auth := &sdk.AuthHelper{
		Required: authMiddleware,
		RequireRole: func(roles ...string) func(http.Handler) http.Handler {
			return authMiddleware
		},
	}

	router := chi.NewRouter()
	mountAdminRoutes(router, syncer, auth, logger)

	// Test that the route is registered.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/meilisearch/reindex", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("status = %d, want %d", w.Code, http.StatusAccepted)
	}
}

func TestMountAdminRoutes_WrongMethod(t *testing.T) {
	engine := newMeilisearchEngineWithProvider(newMockClient(), Config{IndexPrefix: "stoa"}, zerolog.Nop())
	syncer := NewSyncer(nil, engine, Config{BatchSize: 500, IndexPrefix: "stoa"}, zerolog.Nop())
	logger := zerolog.Nop()

	authMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
		})
	}
	auth := &sdk.AuthHelper{
		Required: authMiddleware,
		RequireRole: func(roles ...string) func(http.Handler) http.Handler {
			return authMiddleware
		},
	}

	router := chi.NewRouter()
	mountAdminRoutes(router, syncer, auth, logger)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/meilisearch/reindex", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}
