package n8n

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stoa-hq/stoa/pkg/sdk"
)

func testDispatcher(t *testing.T, baseURL string) *dispatcher {
	t.Helper()
	cfg := Config{
		WebhookBaseURL: baseURL,
		Secret:         "test-secret",
		Timeout:        5 * time.Second,
	}
	return newDispatcher(cfg, zerolog.Nop())
}

func TestDispatcher_Send_PostsToCorrectURL(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := testDispatcher(t, srv.URL+"/webhook/stoa")
	event := &sdk.HookEvent{Name: "order.after_create", Entity: map[string]string{"id": "abc"}}

	if err := d.Send(context.Background(), event); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if gotPath != "/webhook/stoa/order.after_create" {
		t.Errorf("expected path /webhook/stoa/order.after_create, got %s", gotPath)
	}
}

func TestDispatcher_Send_SignsPayload(t *testing.T) {
	var gotSig string
	var gotBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig = r.Header.Get("X-Stoa-Signature")
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := testDispatcher(t, srv.URL)
	event := &sdk.HookEvent{Name: "product.after_update"}

	if err := d.Send(context.Background(), event); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if gotSig == "" {
		t.Fatal("expected X-Stoa-Signature header, got empty string")
	}

	const prefix = "sha256="
	if len(gotSig) <= len(prefix) {
		t.Fatalf("X-Stoa-Signature too short: %q", gotSig)
	}
	_, err := hex.DecodeString(gotSig[len(prefix):])
	if err != nil {
		t.Errorf("X-Stoa-Signature hex portion is not valid hex: %v", err)
	}
}

func TestDispatcher_Send_HTTPErrorReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	d := testDispatcher(t, srv.URL)
	event := &sdk.HookEvent{Name: "order.after_create"}

	if err := d.Send(context.Background(), event); err == nil {
		t.Fatal("expected error for HTTP 500 response, got nil")
	}
}

func TestSign_HMACSHA256(t *testing.T) {
	body := []byte(`{"event":"order.after_create"}`)
	secret := "my-secret"

	got := sign(body, secret)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))

	if got != want {
		t.Errorf("sign() = %q, want %q", got, want)
	}
}

func TestDispatcher_Ping_ReachableServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := testDispatcher(t, srv.URL)
	if err := d.Ping(context.Background()); err != nil {
		t.Errorf("Ping() unexpected error = %v", err)
	}
}

func TestDispatcher_Ping_UnreachableServer(t *testing.T) {
	d := testDispatcher(t, "http://127.0.0.1:1") // nothing listens here
	if err := d.Ping(context.Background()); err == nil {
		t.Error("Ping() expected error for unreachable server, got nil")
	}
}
