package stripe

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stoa-hq/stoa/pkg/sdk"
	stripe "github.com/stripe/stripe-go/v82"
	stripewebhook "github.com/stripe/stripe-go/v82/webhook"
)

func newTestRouter() chi.Router {
	return chi.NewRouter()
}

// signPayload signs a webhook payload and returns the Stripe-Signature header value.
func signPayload(t *testing.T, payload []byte, secret string) string {
	t.Helper()
	signed := stripewebhook.GenerateTestSignedPayload(&stripewebhook.UnsignedPayload{
		Payload:   payload,
		Secret:    secret,
		Timestamp: time.Now(),
	})
	return signed.Header
}

// testAuthHelper returns an AuthHelper for testing.
// If userID is uuid.Nil, UserID returns Nil (simulating unauthenticated).
func testAuthHelper(userID uuid.UUID) *sdk.AuthHelper {
	return &sdk.AuthHelper{
		OptionalAuth: func(next http.Handler) http.Handler { return next },
		Required: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if userID == uuid.Nil {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				next.ServeHTTP(w, r)
			})
		},
		UserID: func(ctx context.Context) uuid.UUID {
			return userID
		},
		UserType: func(ctx context.Context) string {
			if userID == uuid.Nil {
				return ""
			}
			return "customer"
		},
	}
}

func TestPaymentIntentHandler_BadJSON(t *testing.T) {
	sc := &stripeClient{publishableKey: "pk_test", currency: "EUR"}
	auth := testAuthHelper(uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"))
	h := paymentIntentHandler(sc, nil, auth, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("not-json"))
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestPaymentIntentHandler_InvalidOrderID(t *testing.T) {
	sc := &stripeClient{publishableKey: "pk_test", currency: "EUR"}
	auth := testAuthHelper(uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"))
	h := paymentIntentHandler(sc, nil, auth, zerolog.Nop())

	body, _ := json.Marshal(paymentIntentRequest{
		OrderID:         "not-a-uuid",
		PaymentMethodID: "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestPaymentIntentHandler_Unauthenticated(t *testing.T) {
	sc := &stripeClient{publishableKey: "pk_test", currency: "EUR"}
	auth := testAuthHelper(uuid.Nil)
	h := paymentIntentHandler(sc, nil, auth, zerolog.Nop())

	body, _ := json.Marshal(paymentIntentRequest{
		OrderID:         "550e8400-e29b-41d4-a716-446655440000",
		PaymentMethodID: "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHealthHandler_ReturnsOK(t *testing.T) {
	sc := &stripeClient{publishableKey: "pk_test_abc", currency: "EUR"}
	h := healthHandler(sc, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/plugins/stripe/health", nil)
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("status field = %v, want \"ok\"", resp["status"])
	}
	if resp["publishable_key"] != "pk_test_abc" {
		t.Errorf("publishable_key = %v, want \"pk_test_abc\"", resp["publishable_key"])
	}
}

func TestWebhookHandler_InvalidSignature(t *testing.T) {
	h := webhookHandler(nil, nil, "whsec_test", zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/plugins/stripe/webhook",
		bytes.NewBufferString(`{"type":"payment_intent.succeeded"}`))
	req.Header.Set("Stripe-Signature", "invalid")
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestWebhookHandler_UnhandledEventType(t *testing.T) {
	secret := "test_webhook_secret"
	payload := []byte(`{"id":"evt_test","type":"charge.succeeded","data":{"object":{}}}`)
	sig := signPayload(t, payload, secret)

	req := httptest.NewRequest(http.MethodPost, "/plugins/stripe/webhook",
		bytes.NewReader(payload))
	req.Header.Set("Stripe-Signature", sig)
	w := httptest.NewRecorder()

	h := webhookHandler(nil, nil, secret, zerolog.Nop())
	h(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
}

func TestWebhookHandler_ValidSignedPaymentIntentFailed(t *testing.T) {
	secret := "test_pi_failed_secret"
	pi := stripe.PaymentIntent{
		ID:       "pi_test_failed",
		Amount:   1999,
		Currency: "eur",
		Metadata: map[string]string{
			// deliberately missing stoa_order_id — the goroutine will log an error
		},
	}
	piJSON, _ := json.Marshal(pi)
	event := map[string]interface{}{
		"id":   "evt_failed",
		"type": "payment_intent.payment_failed",
		"data": map[string]interface{}{"object": json.RawMessage(piJSON)},
	}
	payload, _ := json.Marshal(event)
	sig := signPayload(t, payload, secret)

	req := httptest.NewRequest(http.MethodPost, "/plugins/stripe/webhook", bytes.NewReader(payload))
	req.Header.Set("Stripe-Signature", sig)
	w := httptest.NewRecorder()

	h := webhookHandler(nil, nil, secret, zerolog.Nop())
	h(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
}

func TestMountRoutes_PaymentIntentRequiresAuth(t *testing.T) {
	// Verify that the payment intent route is protected by auth middleware.
	// The testAuthHelper with uuid.Nil simulates an unauthenticated user;
	// the Required middleware returns 401.
	sc := &stripeClient{publishableKey: "pk_test", currency: "EUR"}
	auth := testAuthHelper(uuid.Nil)

	router := newTestRouter()
	mountRoutes(router, sc, nil, nil, auth, "whsec_test", zerolog.Nop())

	body, _ := json.Marshal(paymentIntentRequest{
		OrderID:         "550e8400-e29b-41d4-a716-446655440000",
		PaymentMethodID: "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/store/stripe/payment-intent", bytes.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated request: status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestMountRoutes_HealthRequiresAuth(t *testing.T) {
	// Verify health endpoint is protected by auth middleware.
	auth := testAuthHelper(uuid.Nil)
	sc := &stripeClient{publishableKey: "pk_test", currency: "EUR"}

	router := newTestRouter()
	mountRoutes(router, sc, nil, nil, auth, "whsec_test", zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/plugins/stripe/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated health check: status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestMountRoutes_WebhookNoAuth(t *testing.T) {
	// Verify webhook endpoint doesn't require auth (only Stripe signature).
	secret := "test_noauth_secret"
	payload := []byte(`{"id":"evt_test","type":"charge.succeeded","data":{"object":{}}}`)
	sig := signPayload(t, payload, secret)

	auth := testAuthHelper(uuid.Nil)
	router := newTestRouter()
	mountRoutes(router, nil, nil, nil, auth, secret, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/plugins/stripe/webhook", bytes.NewReader(payload))
	req.Header.Set("Stripe-Signature", sig)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Should be 204, not 401 — webhook uses signature, not auth middleware.
	if w.Code != http.StatusNoContent {
		t.Errorf("webhook without auth: status = %d, want %d", w.Code, http.StatusNoContent)
	}
}
