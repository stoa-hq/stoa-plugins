package stripe

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"
	stripe "github.com/stripe/stripe-go/v82"
	stripewebhook "github.com/stripe/stripe-go/v82/webhook"
)

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

func TestPaymentIntentHandler_BadJSON(t *testing.T) {
	sc := &stripeClient{publishableKey: "pk_test", currency: "EUR"}
	h := paymentIntentHandler(sc, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("not-json"))
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestPaymentIntentHandler_InvalidOrderID(t *testing.T) {
	sc := &stripeClient{publishableKey: "pk_test", currency: "EUR"}
	h := paymentIntentHandler(sc, nil, zerolog.Nop())

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
	// Build a valid signed webhook payload for an unhandled event type.
	secret := "test_webhook_secret"
	payload := []byte(`{"id":"evt_test","type":"charge.succeeded","data":{"object":{}}}`)
	sig := signPayload(t, payload, secret)

	req := httptest.NewRequest(http.MethodPost, "/plugins/stripe/webhook",
		bytes.NewReader(payload))
	req.Header.Set("Stripe-Signature", sig)
	w := httptest.NewRecorder()

	h := webhookHandler(nil, nil, secret, zerolog.Nop())
	h(w, req)

	// Unhandled events must still return 204 to avoid Stripe retrying.
	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
}

func TestWebhookHandler_ValidSignedPaymentIntentFailed(t *testing.T) {
	// A properly signed payment_intent.payment_failed event with no stoa metadata
	// should be acknowledged (204) since processing runs in a goroutine and
	// errors are only logged.
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
