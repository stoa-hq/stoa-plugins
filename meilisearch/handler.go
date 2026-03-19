package meilisearch

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/stoa-hq/stoa/pkg/sdk"
)

// mountAdminRoutes registers admin HTTP routes for the Meilisearch plugin.
func mountAdminRoutes(router chi.Router, syncer *Syncer, auth *sdk.AuthHelper, logger zerolog.Logger) {
	router.Route("/api/v1/admin/meilisearch", func(r chi.Router) {
		r.Use(auth.Required)
		r.Use(auth.RequireRole("super_admin", "admin", "manager"))
		r.Post("/reindex", reindexHandler(syncer, logger))
	})
}

// reindexHandler triggers a full reindex of all products and categories.
// Only one reindex can run at a time; concurrent requests return 409 Conflict.
func reindexHandler(syncer *Syncer, logger zerolog.Logger) http.HandlerFunc {
	var mu sync.Mutex
	var running bool

	return func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if running {
			mu.Unlock()
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"errors": []map[string]string{
					{"code": "reindex_in_progress", "detail": "a reindex is already running"},
				},
			})
			return
		}
		running = true
		mu.Unlock()

		go func() {
			defer func() {
				mu.Lock()
				running = false
				mu.Unlock()
			}()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			if err := syncer.FullReindex(ctx); err != nil {
				logger.Error().Err(err).Msg("reindex failed")
				return
			}
			logger.Info().Msg("reindex completed")
		}()

		writeJSON(w, http.StatusAccepted, map[string]interface{}{
			"data": map[string]interface{}{
				"status":  "accepted",
				"message": "reindex started",
			},
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}
