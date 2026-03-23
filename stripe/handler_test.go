package stripe

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestPaymentIntentHandler_PreOrder_MissingAmount(t *testing.T) {
	sc := &stripeClient{publishableKey: "pk_test", currency: "EUR"}
	auth := testAuthHelper(uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"))
	h := paymentIntentHandler(sc, nil, auth, zerolog.Nop())

	body, _ := json.Marshal(paymentIntentRequest{
		PaymentMethodID: "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
		Currency:        "EUR",
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestPaymentIntentHandler_PreOrder_MissingCurrency(t *testing.T) {
	sc := &stripeClient{publishableKey: "pk_test", currency: "EUR"}
	auth := testAuthHelper(uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"))
	h := paymentIntentHandler(sc, nil, auth, zerolog.Nop())

	body, _ := json.Marshal(paymentIntentRequest{
		PaymentMethodID: "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
		Amount:          1999,
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
	mountRoutes(router, sc, nil, nil, auth, nil, false, Config{WebhookSecret: "whsec_test"}, zerolog.Nop())

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
	mountRoutes(router, sc, nil, nil, auth, nil, false, Config{WebhookSecret: "whsec_test"}, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/plugins/stripe/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated health check: status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestExtractReceiptEmail_WithEmail(t *testing.T) {
	raw := []byte(`{"first_name":"Max","email":"max@example.com","city":"Berlin"}`)
	got := extractReceiptEmail(raw)
	if got != "max@example.com" {
		t.Errorf("extractReceiptEmail() = %q, want %q", got, "max@example.com")
	}
}

func TestExtractReceiptEmail_NoEmail(t *testing.T) {
	raw := []byte(`{"first_name":"Max","city":"Berlin"}`)
	got := extractReceiptEmail(raw)
	if got != "" {
		t.Errorf("extractReceiptEmail() = %q, want empty", got)
	}
}

func TestExtractReceiptEmail_Nil(t *testing.T) {
	got := extractReceiptEmail(nil)
	if got != "" {
		t.Errorf("extractReceiptEmail(nil) = %q, want empty", got)
	}
}

func TestExtractReceiptEmail_InvalidJSON(t *testing.T) {
	raw := []byte(`not-json`)
	got := extractReceiptEmail(raw)
	if got != "" {
		t.Errorf("extractReceiptEmail(invalid) = %q, want empty", got)
	}
}

func TestExtractReceiptEmail_EmptyJSON(t *testing.T) {
	raw := []byte(`{}`)
	got := extractReceiptEmail(raw)
	if got != "" {
		t.Errorf("extractReceiptEmail({}) = %q, want empty", got)
	}
}

// --- paymentLinkCreateHandler tests ---

func TestPaymentLinkCreateHandler_BadJSON(t *testing.T) {
	sc := &stripeClient{publishableKey: "pk_test", currency: "EUR"}
	auth := testAuthHelper(uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"))
	h := paymentLinkCreateHandler(sc, nil, auth, Config{Currency: "EUR"}, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("not-json"))
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestPaymentLinkCreateHandler_InvalidCartID(t *testing.T) {
	sc := &stripeClient{publishableKey: "pk_test", currency: "EUR"}
	auth := testAuthHelper(uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"))
	h := paymentLinkCreateHandler(sc, nil, auth, Config{Currency: "EUR"}, zerolog.Nop())

	body, _ := json.Marshal(paymentLinkCreateRequest{
		CartID:           "not-a-uuid",
		PaymentMethodID:  "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
		ShippingMethodID: "6ba7b810-9dad-11d1-80b4-00c04fd430c9",
		ShippingAddress:  json.RawMessage(`{"city":"Berlin"}`),
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestPaymentLinkCreateHandler_InvalidPaymentMethodID(t *testing.T) {
	sc := &stripeClient{publishableKey: "pk_test", currency: "EUR"}
	auth := testAuthHelper(uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"))
	h := paymentLinkCreateHandler(sc, nil, auth, Config{Currency: "EUR"}, zerolog.Nop())

	body, _ := json.Marshal(paymentLinkCreateRequest{
		CartID:           "550e8400-e29b-41d4-a716-446655440001",
		PaymentMethodID:  "not-a-uuid",
		ShippingMethodID: "6ba7b810-9dad-11d1-80b4-00c04fd430c9",
		ShippingAddress:  json.RawMessage(`{"city":"Berlin"}`),
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestPaymentLinkCreateHandler_InvalidShippingMethodID(t *testing.T) {
	sc := &stripeClient{publishableKey: "pk_test", currency: "EUR"}
	auth := testAuthHelper(uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"))
	h := paymentLinkCreateHandler(sc, nil, auth, Config{Currency: "EUR"}, zerolog.Nop())

	body, _ := json.Marshal(paymentLinkCreateRequest{
		CartID:           "550e8400-e29b-41d4-a716-446655440001",
		PaymentMethodID:  "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
		ShippingMethodID: "not-a-uuid",
		ShippingAddress:  json.RawMessage(`{"city":"Berlin"}`),
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestPaymentLinkCreateHandler_MissingShippingAddress(t *testing.T) {
	sc := &stripeClient{publishableKey: "pk_test", currency: "EUR"}
	auth := testAuthHelper(uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"))
	h := paymentLinkCreateHandler(sc, nil, auth, Config{Currency: "EUR"}, zerolog.Nop())

	body, _ := json.Marshal(map[string]interface{}{
		"cart_id":            "550e8400-e29b-41d4-a716-446655440001",
		"payment_method_id":  "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
		"shipping_method_id": "6ba7b810-9dad-11d1-80b4-00c04fd430c9",
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestPaymentLinkCreateHandler_CartTotalError(t *testing.T) {
	// db is nil — calculateCartTotal will fail → handler should return 422.
	sc := &stripeClient{publishableKey: "pk_test", currency: "EUR"}
	auth := testAuthHelper(uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"))
	h := paymentLinkCreateHandler(sc, nil, auth, Config{Currency: "EUR"}, zerolog.Nop())

	body, _ := json.Marshal(paymentLinkCreateRequest{
		CartID:           "550e8400-e29b-41d4-a716-446655440001",
		PaymentMethodID:  "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
		ShippingMethodID: "6ba7b810-9dad-11d1-80b4-00c04fd430c9",
		ShippingAddress:  json.RawMessage(`{"city":"Berlin"}`),
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
}

// --- paymentLinkGetHandler tests ---

func TestPaymentLinkGetHandler_MissingToken(t *testing.T) {
	// Token param is empty — chi won't match the route, but test the handler directly.
	h := paymentLinkGetHandler(nil, zerolog.Nop())

	// Build request with chi context but empty token.
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("token", "")
	req := httptest.NewRequest(http.MethodGet, "/payment-link/", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// --- paymentLinkCompleteHandler tests ---

func TestPaymentLinkCompleteHandler_BadJSON(t *testing.T) {
	sc := &stripeClient{publishableKey: "pk_test", currency: "EUR"}
	h := paymentLinkCompleteHandler(sc, nil, nil, false, zerolog.Nop())

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("token", "some-token")
	req := httptest.NewRequest(http.MethodPost, "/payment-link/some-token/complete", bytes.NewBufferString("bad"))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestPaymentLinkCompleteHandler_MissingPaymentIntentID(t *testing.T) {
	sc := &stripeClient{publishableKey: "pk_test", currency: "EUR"}
	h := paymentLinkCompleteHandler(sc, nil, nil, false, zerolog.Nop())

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("token", "some-token")
	body, _ := json.Marshal(paymentLinkCompleteRequest{PaymentIntentID: ""})
	req := httptest.NewRequest(http.MethodPost, "/payment-link/some-token/complete", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestPaymentLinkCompleteHandler_MissingToken(t *testing.T) {
	sc := &stripeClient{publishableKey: "pk_test", currency: "EUR"}
	h := paymentLinkCompleteHandler(sc, nil, nil, false, zerolog.Nop())

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("token", "")
	body, _ := json.Marshal(paymentLinkCompleteRequest{PaymentIntentID: "pi_test"})
	req := httptest.NewRequest(http.MethodPost, "/payment-link//complete", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// --- paymentStatusHandler tests ---

func TestPaymentStatusHandler_MissingPaymentIntentID(t *testing.T) {
	sc := &stripeClient{publishableKey: "pk_test", currency: "EUR"}
	auth := testAuthHelper(uuid.Nil)
	h := paymentStatusHandler(sc, nil, auth, zerolog.Nop())

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("paymentIntentID", "")
	req := httptest.NewRequest(http.MethodGet, "/payment-status/", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestPaymentStatusHandler_StripeError(t *testing.T) {
	// Stripe returns an error — handler should return 502.
	_, sc := newTestStripeClient(t, http.StatusInternalServerError, `{"error":{"type":"api_error","message":"internal"}}`)
	auth := testAuthHelper(uuid.Nil)
	h := paymentStatusHandler(sc, nil, auth, zerolog.Nop())

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("paymentIntentID", "pi_test_123")
	req := httptest.NewRequest(http.MethodGet, "/payment-status/pi_test_123", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
}

func TestPaymentStatusHandler_Success(t *testing.T) {
	// Stripe returns a valid PI with matching customer ID in metadata.
	customerID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	piJSON := `{"id":"pi_test_ok","amount":1999,"currency":"eur","status":"requires_capture","client_secret":"cs_test","metadata":{"stoa_customer_id":"22222222-2222-2222-2222-222222222222"}}`
	_, sc := newTestStripeClient(t, http.StatusOK, piJSON)
	auth := testAuthHelper(customerID)
	h := paymentStatusHandler(sc, nil, auth, zerolog.Nop())

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("paymentIntentID", "pi_test_ok")
	req := httptest.NewRequest(http.MethodGet, "/payment-status/pi_test_ok", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	data := resp["data"]
	if data == nil {
		t.Fatal("response missing 'data' field")
	}
	if data["status"] != "requires_capture" {
		t.Errorf("data.status = %v, want %q", data["status"], "requires_capture")
	}
}

func TestPaymentStatusHandler_IDOR_WrongCustomer(t *testing.T) {
	// PI belongs to a different customer — should return 404.
	piJSON := `{"id":"pi_test_ok","amount":1999,"currency":"eur","status":"requires_capture","metadata":{"stoa_customer_id":"11111111-1111-1111-1111-111111111111"}}`
	_, sc := newTestStripeClient(t, http.StatusOK, piJSON)
	wrongCustomer := uuid.MustParse("99999999-9999-9999-9999-999999999999")
	auth := testAuthHelper(wrongCustomer)
	h := paymentStatusHandler(sc, nil, auth, zerolog.Nop())

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("paymentIntentID", "pi_test_ok")
	req := httptest.NewRequest(http.MethodGet, "/payment-status/pi_test_ok", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("IDOR check: status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestPaymentStatusHandler_GuestWithoutToken(t *testing.T) {
	// Unauthenticated user without guest token — should return 404.
	piJSON := `{"id":"pi_test_ok","amount":1999,"currency":"eur","status":"succeeded","metadata":{"stoa_guest_token":"secret123"}}`
	_, sc := newTestStripeClient(t, http.StatusOK, piJSON)
	auth := testAuthHelper(uuid.Nil)
	h := paymentStatusHandler(sc, nil, auth, zerolog.Nop())

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("paymentIntentID", "pi_test_ok")
	req := httptest.NewRequest(http.MethodGet, "/payment-status/pi_test_ok", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("guest without token: status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// --- paymentPageHandler tests ---

func TestPaymentPageHandler_HasCSPHeader(t *testing.T) {
	h := paymentPageHandler()

	req := httptest.NewRequest(http.MethodGet, "/plugins/stripe/pay/some-token", nil)
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	csp := w.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("Content-Security-Policy header is missing")
	}
}

func TestPaymentPageHandler_CSPContainsNonce(t *testing.T) {
	h := paymentPageHandler()

	req := httptest.NewRequest(http.MethodGet, "/plugins/stripe/pay/some-token", nil)
	w := httptest.NewRecorder()
	h(w, req)

	csp := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "nonce-") {
		t.Errorf("CSP does not contain nonce-: %q", csp)
	}
}

func TestPaymentPageHandler_CSPScriptSrc(t *testing.T) {
	h := paymentPageHandler()

	req := httptest.NewRequest(http.MethodGet, "/plugins/stripe/pay/some-token", nil)
	w := httptest.NewRecorder()
	h(w, req)

	csp := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src") {
		t.Errorf("CSP missing script-src directive: %q", csp)
	}
	if !strings.Contains(csp, "https://js.stripe.com") {
		t.Errorf("CSP script-src missing https://js.stripe.com: %q", csp)
	}
}

func TestPaymentPageHandler_CSPStyleSrc(t *testing.T) {
	h := paymentPageHandler()

	req := httptest.NewRequest(http.MethodGet, "/plugins/stripe/pay/some-token", nil)
	w := httptest.NewRecorder()
	h(w, req)

	csp := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "style-src") {
		t.Errorf("CSP missing style-src directive: %q", csp)
	}
}

func TestPaymentPageHandler_CSPConnectSrc(t *testing.T) {
	h := paymentPageHandler()

	req := httptest.NewRequest(http.MethodGet, "/plugins/stripe/pay/some-token", nil)
	w := httptest.NewRecorder()
	h(w, req)

	csp := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "https://api.stripe.com") {
		t.Errorf("CSP connect-src missing https://api.stripe.com: %q", csp)
	}
}

func TestPaymentPageHandler_BodyContainsNonceAttributes(t *testing.T) {
	h := paymentPageHandler()

	req := httptest.NewRequest(http.MethodGet, "/plugins/stripe/pay/some-token", nil)
	w := httptest.NewRecorder()
	h(w, req)

	body := w.Body.String()
	if !strings.Contains(body, `nonce="`) {
		t.Error("response body does not contain nonce= attributes on script/style tags")
	}
}

func TestPaymentPageHandler_NonceMatchesBetweenCSPAndBody(t *testing.T) {
	h := paymentPageHandler()

	req := httptest.NewRequest(http.MethodGet, "/plugins/stripe/pay/some-token", nil)
	w := httptest.NewRecorder()
	h(w, req)

	csp := w.Header().Get("Content-Security-Policy")
	body := w.Body.String()

	// Extract nonce from CSP: "nonce-<value>"
	const prefix = "nonce-"
	nonceStart := strings.Index(csp, prefix)
	if nonceStart == -1 {
		t.Fatal("nonce not found in CSP header")
	}
	nonceStart += len(prefix)
	nonceEnd := strings.IndexAny(csp[nonceStart:], "; '\"")
	var nonce string
	if nonceEnd == -1 {
		nonce = csp[nonceStart:]
	} else {
		nonce = csp[nonceStart : nonceStart+nonceEnd]
	}

	if !strings.Contains(body, `nonce="`+nonce+`"`) {
		t.Errorf("body does not contain nonce %q from CSP header", nonce)
	}
}

func TestPaymentPageHandler_EachRequestGetsDifferentNonce(t *testing.T) {
	h := paymentPageHandler()

	req1 := httptest.NewRequest(http.MethodGet, "/plugins/stripe/pay/tok1", nil)
	w1 := httptest.NewRecorder()
	h(w1, req1)

	req2 := httptest.NewRequest(http.MethodGet, "/plugins/stripe/pay/tok2", nil)
	w2 := httptest.NewRecorder()
	h(w2, req2)

	csp1 := w1.Header().Get("Content-Security-Policy")
	csp2 := w2.Header().Get("Content-Security-Policy")

	if csp1 == csp2 {
		t.Error("two separate requests produced identical CSP headers (nonces should differ)")
	}
}

func TestPaymentPageHandler_CacheControlNoStore(t *testing.T) {
	h := paymentPageHandler()

	req := httptest.NewRequest(http.MethodGet, "/plugins/stripe/pay/some-token", nil)
	w := httptest.NewRecorder()
	h(w, req)

	cc := w.Header().Get("Cache-Control")
	if cc != "no-store" {
		t.Errorf("Cache-Control = %q, want %q", cc, "no-store")
	}
}

func TestMountRoutes_WebhookNoAuth(t *testing.T) {
	// Verify webhook endpoint doesn't require auth (only Stripe signature).
	secret := "test_noauth_secret"
	payload := []byte(`{"id":"evt_test","type":"charge.succeeded","data":{"object":{}}}`)
	sig := signPayload(t, payload, secret)

	auth := testAuthHelper(uuid.Nil)
	router := newTestRouter()
	mountRoutes(router, nil, nil, nil, auth, nil, false, Config{WebhookSecret: secret}, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/plugins/stripe/webhook", bytes.NewReader(payload))
	req.Header.Set("Stripe-Signature", sig)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Should be 204, not 401 — webhook uses signature, not auth middleware.
	if w.Code != http.StatusNoContent {
		t.Errorf("webhook without auth: status = %d, want %d", w.Code, http.StatusNoContent)
	}
}
