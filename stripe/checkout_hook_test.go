package stripe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stoa-hq/stoa/pkg/sdk"
)

// ---------------------------------------------------------------------------
// validateCheckoutPayment
// ---------------------------------------------------------------------------

func TestValidateCheckoutPayment_NonStripeProvider(t *testing.T) {
	event := &sdk.HookEvent{
		Metadata: map[string]interface{}{
			"provider":          "manual",
			"payment_reference": "",
		},
	}
	if err := validateCheckoutPayment(context.Background(), event, nil, zerolog.Nop()); err != nil {
		t.Errorf("non-stripe provider should be skipped, got error: %v", err)
	}
}

func TestValidateCheckoutPayment_EmptyProvider(t *testing.T) {
	event := &sdk.HookEvent{
		Metadata: map[string]interface{}{
			"provider":          "",
			"payment_reference": "",
		},
	}
	if err := validateCheckoutPayment(context.Background(), event, nil, zerolog.Nop()); err != nil {
		t.Errorf("empty provider should be skipped, got error: %v", err)
	}
}

func TestValidateCheckoutPayment_StripeProvider_EmptyReference(t *testing.T) {
	event := &sdk.HookEvent{
		Metadata: map[string]interface{}{
			"provider":          "stripe",
			"payment_reference": "",
		},
	}
	err := validateCheckoutPayment(context.Background(), event, nil, zerolog.Nop())
	if err == nil {
		t.Error("expected error for empty payment_reference with stripe provider")
	}
}

func TestValidateCheckoutPayment_NilMetadata(t *testing.T) {
	event := &sdk.HookEvent{}
	// No metadata at all — provider will be "" → skip
	if err := validateCheckoutPayment(context.Background(), event, nil, zerolog.Nop()); err != nil {
		t.Errorf("nil metadata should be skipped, got error: %v", err)
	}
}

func TestValidateCheckoutPayment_RequiresCapture(t *testing.T) {
	_, sc := newTestStripeClient(t, http.StatusOK,
		`{"id":"pi_test123","object":"payment_intent","status":"requires_capture","amount":1999,"currency":"eur"}`)

	event := &sdk.HookEvent{
		Metadata: map[string]interface{}{
			"provider":          "stripe",
			"payment_reference": "pi_test123",
		},
	}
	if err := validateCheckoutPayment(context.Background(), event, sc, zerolog.Nop()); err != nil {
		t.Errorf("requires_capture should be accepted, got error: %v", err)
	}
}

func TestValidateCheckoutPayment_SucceededStillAccepted(t *testing.T) {
	_, sc := newTestStripeClient(t, http.StatusOK,
		`{"id":"pi_test123","object":"payment_intent","status":"succeeded","amount":1999,"currency":"eur"}`)

	event := &sdk.HookEvent{
		Metadata: map[string]interface{}{
			"provider":          "stripe",
			"payment_reference": "pi_test123",
		},
	}
	if err := validateCheckoutPayment(context.Background(), event, sc, zerolog.Nop()); err != nil {
		t.Errorf("succeeded should still be accepted, got error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// handleFailedCheckoutPayment (cancel instead of refund)
// ---------------------------------------------------------------------------

func TestHandleFailedCheckoutPayment_NonStripeProvider(t *testing.T) {
	event := &sdk.HookEvent{
		Metadata: map[string]interface{}{
			"provider":          "manual",
			"payment_reference": "pi_test123",
		},
	}
	// sc is nil — any API call would panic; expect early return without panic.
	handleFailedCheckoutPayment(context.Background(), event, nil, zerolog.Nop())
}

func TestHandleFailedCheckoutPayment_EmptyPaymentReference(t *testing.T) {
	event := &sdk.HookEvent{
		Metadata: map[string]interface{}{
			"provider":          "stripe",
			"payment_reference": "",
		},
	}
	// sc is nil — any API call would panic; expect early return without panic.
	handleFailedCheckoutPayment(context.Background(), event, nil, zerolog.Nop())
}

func TestHandleFailedCheckoutPayment_APIError_LogsAndDoesNotPanic(t *testing.T) {
	_, sc := newTestStripeClient(t, http.StatusBadRequest,
		`{"error":{"type":"invalid_request_error","message":"No such payment_intent: pi_notfound"}}`)

	event := &sdk.HookEvent{
		Metadata: map[string]interface{}{
			"provider":          "stripe",
			"payment_reference": "pi_notfound",
		},
	}
	// Should log error but not panic.
	handleFailedCheckoutPayment(context.Background(), event, sc, zerolog.Nop())
}

func TestHandleFailedCheckoutPayment_Success_CallsCancelAPI(t *testing.T) {
	called := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"pi_test123","object":"payment_intent","status":"canceled","amount":2000,"currency":"eur"}`))
	}))
	t.Cleanup(ts.Close)
	sc := newTestStripeClientFromServer(t, ts)

	event := &sdk.HookEvent{
		Metadata: map[string]interface{}{
			"provider":          "stripe",
			"payment_reference": "pi_test123",
		},
	}
	handleFailedCheckoutPayment(context.Background(), event, sc, zerolog.Nop())

	if !called {
		t.Error("expected Stripe PaymentIntents Cancel API to be called")
	}
}

// ---------------------------------------------------------------------------
// captureOnStatusChange
// ---------------------------------------------------------------------------

func TestCaptureOnStatusChange_WrongStatus(t *testing.T) {
	event := &sdk.HookEvent{
		Changes: map[string]interface{}{
			"to_status":         "confirmed",
			"payment_reference": "pi_test123",
		},
	}
	// captureOn = "shipped" → "confirmed" doesn't match → no API call
	// sc is nil so any call would panic.
	captureOnStatusChange(context.Background(), event, nil, nil, "shipped", zerolog.Nop())
}

func TestCaptureOnStatusChange_EmptyRef(t *testing.T) {
	event := &sdk.HookEvent{
		Changes: map[string]interface{}{
			"to_status":         "shipped",
			"payment_reference": "",
		},
	}
	// No payment_reference → early return, no panic.
	captureOnStatusChange(context.Background(), event, nil, nil, "shipped", zerolog.Nop())
}

func TestCaptureOnStatusChange_APIError(t *testing.T) {
	_, sc := newTestStripeClient(t, http.StatusBadRequest,
		`{"error":{"type":"invalid_request_error","message":"No such payment_intent: pi_notfound"}}`)

	event := &sdk.HookEvent{
		Changes: map[string]interface{}{
			"to_status":         "shipped",
			"payment_reference": "pi_notfound",
		},
	}
	// Should log error but not panic.
	captureOnStatusChange(context.Background(), event, sc, nil, "shipped", zerolog.Nop())
}

func TestCaptureOnStatusChange_Success(t *testing.T) {
	captureAPICalled := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureAPICalled = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"pi_test123","object":"payment_intent","status":"succeeded","amount":1999,"currency":"eur"}`))
	}))
	t.Cleanup(ts.Close)
	sc := newTestStripeClientFromServer(t, ts)

	event := &sdk.HookEvent{
		Changes: map[string]interface{}{
			"to_status":         "shipped",
			"payment_reference": "pi_test123",
		},
	}
	// db is nil — the DB update will fail, but we just check that the Capture API was called.
	captureOnStatusChange(context.Background(), event, sc, nil, "shipped", zerolog.Nop())

	if !captureAPICalled {
		t.Error("expected Stripe PaymentIntents Capture API to be called")
	}
}
