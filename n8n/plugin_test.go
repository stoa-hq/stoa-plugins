package n8n

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stoa-hq/stoa/pkg/sdk"
)

// noopAuth returns an AuthHelper that always passes authentication and returns
// a fixed user ID. Use for tests that don't exercise auth logic.
func noopAuth() *sdk.AuthHelper {
	passthrough := func(next http.Handler) http.Handler { return next }
	return &sdk.AuthHelper{
		OptionalAuth: passthrough,
		Required:     passthrough,
		UserID:       func(ctx context.Context) uuid.UUID { return uuid.New() },
		UserType:     func(ctx context.Context) string { return "admin" },
	}
}

func testAppContext(t *testing.T, webhookBaseURL string) *sdk.AppContext {
	t.Helper()
	return &sdk.AppContext{
		Router: chi.NewRouter(),
		Hooks:  sdk.NewHookRegistry(),
		Logger: zerolog.Nop(),
		Auth:   noopAuth(),
		Config: map[string]interface{}{
			"n8n": map[string]interface{}{
				"webhook_base_url": webhookBaseURL,
				"secret":           "test-secret",
				"timeout_seconds":  float64(5),
			},
		},
	}
}

func TestPlugin_Init_RegistersHooks(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := New()
	app := testAppContext(t, srv.URL)

	if err := p.Init(app); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// Dispatch an after-hook — should trigger the plugin's handler.
	event := &sdk.HookEvent{Name: sdk.HookAfterOrderCreate, Entity: map[string]string{"id": "1"}}
	if err := app.Hooks.Dispatch(context.Background(), event); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	// Give the HTTP call time to complete (it's synchronous, but be safe).
	time.Sleep(50 * time.Millisecond)

	if callCount.Load() == 0 {
		t.Error("expected at least one webhook call after hook dispatch")
	}
}

func TestPlugin_Init_BeforeHookNotRegistered(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := New()
	app := testAppContext(t, srv.URL)

	if err := p.Init(app); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// Before-hooks must NOT be forwarded to n8n (they can abort operations).
	event := &sdk.HookEvent{Name: sdk.HookBeforeOrderCreate}
	_ = app.Hooks.Dispatch(context.Background(), event)

	time.Sleep(50 * time.Millisecond)

	if callCount.Load() != 0 {
		t.Errorf("before-hook must not trigger webhook dispatch, got %d calls", callCount.Load())
	}
}

func TestPlugin_Init_InvalidConfig(t *testing.T) {
	p := New()
	app := &sdk.AppContext{
		Router: chi.NewRouter(),
		Hooks:  sdk.NewHookRegistry(),
		Logger: zerolog.Nop(),
		Auth:   noopAuth(),
		Config: map[string]interface{}{}, // missing n8n section
	}

	if err := p.Init(app); err == nil {
		t.Fatal("expected error for missing config, got nil")
	}
}

func TestPlugin_Metadata(t *testing.T) {
	p := New()
	if p.Name() != "n8n" {
		t.Errorf("Name() = %q", p.Name())
	}
	if p.Version() == "" {
		t.Error("Version() should not be empty")
	}
	if p.Description() == "" {
		t.Error("Description() should not be empty")
	}
}

func TestPlugin_Shutdown(t *testing.T) {
	p := New()
	if err := p.Shutdown(); err != nil {
		t.Errorf("Shutdown() unexpected error = %v", err)
	}
}
