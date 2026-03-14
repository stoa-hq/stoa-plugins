package n8n

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/stoa-hq/stoa/pkg/sdk"
)

// mountRoutes registers admin routes for the n8n plugin under /plugins/n8n.
// The health endpoint requires authentication — the plugin router is the ROOT
// Chi router and does NOT inherit Stoa's middleware.
func mountRoutes(router chi.Router, auth *sdk.AuthHelper, d *dispatcher, logger zerolog.Logger) {
	router.Route("/plugins/n8n", func(r chi.Router) {
		r.Use(auth.Required)
		r.Get("/health", healthHandler(d, logger))
	})
}

type healthResponse struct {
	Status       string `json:"status"`
	N8nReachable bool   `json:"n8n_reachable"`
	Error        string `json:"error,omitempty"`
	CheckedAt    string `json:"checked_at"`
}

// healthHandler checks whether n8n is reachable and reports plugin status.
// GET /plugins/n8n/health
func healthHandler(d *dispatcher, logger zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		resp := healthResponse{
			Status:       "ok",
			N8nReachable: true,
			CheckedAt:    time.Now().UTC().Format(time.RFC3339),
		}

		if err := d.Ping(ctx); err != nil {
			logger.Warn().Err(err).Msg("n8n health check failed")
			resp.Status = "degraded"
			resp.N8nReachable = false
			resp.Error = "n8n instance is not reachable"
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}
