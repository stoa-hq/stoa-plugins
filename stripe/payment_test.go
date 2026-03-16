package stripe

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
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

func TestCreatePreOrderPaymentIntent_CustomerIDInMetadata(t *testing.T) {
	var body string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		body = string(bodyBytes)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"pi_test","object":"payment_intent","amount":1999,"currency":"eur","client_secret":"pi_test_secret","status":"requires_payment_method"}`))
	}))
	t.Cleanup(ts.Close)
	sc := newTestStripeClientFromServer(t, ts)

	customerID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	pmID := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	_, err := sc.CreatePreOrderPaymentIntent(context.Background(), pmID, 1999, "EUR", customerID, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(body, "stoa_customer_id") {
		t.Error("expected stoa_customer_id in request body")
	}
	if strings.Contains(body, "stoa_guest_token") {
		t.Error("stoa_guest_token should not be present when empty")
	}
}

func TestCreatePreOrderPaymentIntent_GuestTokenInMetadata(t *testing.T) {
	var body string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		body = string(bodyBytes)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"pi_test","object":"payment_intent","amount":1999,"currency":"eur","client_secret":"pi_test_secret","status":"requires_payment_method"}`))
	}))
	t.Cleanup(ts.Close)
	sc := newTestStripeClientFromServer(t, ts)

	pmID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	guestToken := "abc-guest-123"

	_, err := sc.CreatePreOrderPaymentIntent(context.Background(), pmID, 1999, "EUR", uuid.Nil, guestToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(body, "stoa_guest_token") {
		t.Error("expected stoa_guest_token in request body")
	}
	if strings.Contains(body, "stoa_customer_id") {
		t.Error("stoa_customer_id should not be present when uuid.Nil")
	}
}
