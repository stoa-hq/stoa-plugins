package n8n

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestHealthHandler_N8nReachable(t *testing.T) {
	n8nSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer n8nSrv.Close()

	d := testDispatcher(t, n8nSrv.URL)

	req := httptest.NewRequest(http.MethodGet, "/plugins/n8n/health", nil)
	rec := httptest.NewRecorder()

	healthHandler(d, zerolog.Nop())(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var resp healthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("status = %q, want \"ok\"", resp.Status)
	}
	if !resp.N8nReachable {
		t.Error("n8n_reachable should be true")
	}
}

func TestHealthHandler_N8nUnreachable(t *testing.T) {
	cfg := Config{
		WebhookBaseURL: "http://127.0.0.1:1",
		Secret:         "s",
		Timeout:        1 * time.Second,
	}
	d := newDispatcher(cfg, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/plugins/n8n/health", nil)
	rec := httptest.NewRecorder()

	healthHandler(d, zerolog.Nop())(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}

	var resp healthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "degraded" {
		t.Errorf("status = %q, want \"degraded\"", resp.Status)
	}
	if resp.N8nReachable {
		t.Error("n8n_reachable should be false")
	}
	if resp.Error == "" {
		t.Error("error field should be set")
	}
}

func TestHealthHandler_CheckedAtIsRFC3339(t *testing.T) {
	n8nSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer n8nSrv.Close()

	d := testDispatcher(t, n8nSrv.URL)

	req := httptest.NewRequest(http.MethodGet, "/plugins/n8n/health", nil)
	rec := httptest.NewRecorder()
	healthHandler(d, zerolog.Nop())(rec, req)

	var resp healthResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	if _, err := time.Parse(time.RFC3339, resp.CheckedAt); err != nil {
		t.Errorf("checked_at %q is not RFC3339: %v", resp.CheckedAt, err)
	}
}
