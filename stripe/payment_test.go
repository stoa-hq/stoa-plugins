package stripe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDashboardURL_TestMode(t *testing.T) {
	sc := &stripeClient{publishableKey: "pk_test_abc123"}
	got := sc.DashboardURL("pi_3ABC")
	want := "https://dashboard.stripe.com/test/payments/pi_3ABC"
	if got != want {
		t.Errorf("DashboardURL() = %q, want %q", got, want)
	}
}

func TestDashboardURL_LiveMode(t *testing.T) {
	sc := &stripeClient{publishableKey: "pk_live_abc123"}
	got := sc.DashboardURL("pi_3ABC")
	want := "https://dashboard.stripe.com/payments/pi_3ABC"
	if got != want {
		t.Errorf("DashboardURL() = %q, want %q", got, want)
	}
}

func TestDashboardURL_EmptyID(t *testing.T) {
	sc := &stripeClient{publishableKey: "pk_test_abc123"}
	got := sc.DashboardURL("")
	if got != "" {
		t.Errorf("DashboardURL(\"\") = %q, want empty string", got)
	}
}

func TestRefundPaymentIntent_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"re_test123","object":"refund","amount":1999,"currency":"eur","status":"succeeded"}`))
	}))
	t.Cleanup(ts.Close)
	sc := newTestStripeClientFromServer(t, ts)

	if err := sc.RefundPaymentIntent(context.Background(), "pi_test123"); err != nil {
		t.Errorf("RefundPaymentIntent() unexpected error: %v", err)
	}
}

func TestRefundPaymentIntent_APIError(t *testing.T) {
	_, sc := newTestStripeClient(t, http.StatusBadRequest,
		`{"error":{"type":"invalid_request_error","message":"No such payment_intent: pi_notfound"}}`)

	err := sc.RefundPaymentIntent(context.Background(), "pi_notfound")
	if err == nil {
		t.Error("RefundPaymentIntent() expected error for API failure, got nil")
	}
}

func TestCapturePaymentIntent_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"pi_test123","object":"payment_intent","amount":1999,"currency":"eur","status":"succeeded"}`))
	}))
	t.Cleanup(ts.Close)
	sc := newTestStripeClientFromServer(t, ts)

	if err := sc.CapturePaymentIntent(context.Background(), "pi_test123"); err != nil {
		t.Errorf("CapturePaymentIntent() unexpected error: %v", err)
	}
}

func TestCapturePaymentIntent_APIError(t *testing.T) {
	_, sc := newTestStripeClient(t, http.StatusBadRequest,
		`{"error":{"type":"invalid_request_error","message":"No such payment_intent: pi_notfound"}}`)

	err := sc.CapturePaymentIntent(context.Background(), "pi_notfound")
	if err == nil {
		t.Error("CapturePaymentIntent() expected error for API failure, got nil")
	}
}

func TestCancelPaymentIntent_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"pi_test123","object":"payment_intent","amount":1999,"currency":"eur","status":"canceled"}`))
	}))
	t.Cleanup(ts.Close)
	sc := newTestStripeClientFromServer(t, ts)

	if err := sc.CancelPaymentIntent(context.Background(), "pi_test123"); err != nil {
		t.Errorf("CancelPaymentIntent() unexpected error: %v", err)
	}
}

func TestCancelPaymentIntent_APIError(t *testing.T) {
	_, sc := newTestStripeClient(t, http.StatusBadRequest,
		`{"error":{"type":"invalid_request_error","message":"No such payment_intent: pi_notfound"}}`)

	err := sc.CancelPaymentIntent(context.Background(), "pi_notfound")
	if err == nil {
		t.Error("CancelPaymentIntent() expected error for API failure, got nil")
	}
}
