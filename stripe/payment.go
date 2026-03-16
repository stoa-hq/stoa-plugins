package stripe

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	stripe "github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/client"
)

// stripeClient wraps the Stripe API client.
type stripeClient struct {
	api            *client.API
	publishableKey string
	currency       string
}

func newStripeClient(cfg Config) *stripeClient {
	sc := &client.API{}
	sc.Init(cfg.SecretKey, nil)
	return &stripeClient{
		api:            sc,
		publishableKey: cfg.PublishableKey,
		currency:       cfg.Currency,
	}
}

// PaymentIntentResult holds the data returned after creating a PaymentIntent.
type PaymentIntentResult struct {
	ID             string `json:"id"`
	ClientSecret   string `json:"client_secret"`
	PublishableKey string `json:"publishable_key"`
	Amount         int64  `json:"amount"`
	Currency       string `json:"currency"`
}

// OrderContext holds human-readable order data for enriching the Stripe PaymentIntent.
type OrderContext struct {
	OrderNumber  string
	ReceiptEmail string
}

// CreatePaymentIntent creates a Stripe PaymentIntent for the given order.
// The orderID and paymentMethodID are stored in the PaymentIntent metadata so
// they can be recovered when Stripe fires the webhook event.
func (s *stripeClient) CreatePaymentIntent(
	_ context.Context,
	orderID uuid.UUID,
	paymentMethodID uuid.UUID,
	amount int64,
	currency string,
	oc OrderContext,
) (*PaymentIntentResult, error) {
	if currency == "" {
		currency = s.currency
	}

	metadata := map[string]string{
		"stoa_order_id":          orderID.String(),
		"stoa_payment_method_id": paymentMethodID.String(),
	}
	if oc.OrderNumber != "" {
		metadata["stoa_order_number"] = oc.OrderNumber
	}

	params := &stripe.PaymentIntentParams{
		Amount:   stripe.Int64(amount),
		Currency: stripe.String(strings.ToLower(currency)),
		AutomaticPaymentMethods: &stripe.PaymentIntentAutomaticPaymentMethodsParams{
			Enabled: stripe.Bool(true),
		},
		Metadata: metadata,
	}

	if oc.OrderNumber != "" {
		params.Description = stripe.String("Stoa Order " + oc.OrderNumber)
	}
	if oc.ReceiptEmail != "" {
		params.ReceiptEmail = stripe.String(oc.ReceiptEmail)
	}

	pi, err := s.api.PaymentIntents.New(params)
	if err != nil {
		return nil, fmt.Errorf("stripe: create payment intent: %w", err)
	}

	return &PaymentIntentResult{
		ID:             pi.ID,
		ClientSecret:   pi.ClientSecret,
		PublishableKey: s.publishableKey,
		Amount:         pi.Amount,
		Currency:       string(pi.Currency),
	}, nil
}

// RetrievePaymentIntent fetches a PaymentIntent from Stripe by its ID.
func (s *stripeClient) RetrievePaymentIntent(_ context.Context, id string) (*stripe.PaymentIntent, error) {
	pi, err := s.api.PaymentIntents.Get(id, nil)
	if err != nil {
		return nil, fmt.Errorf("stripe: retrieve payment intent %s: %w", id, err)
	}
	return pi, nil
}

// CreatePreOrderPaymentIntent creates a Stripe PaymentIntent before an order exists.
// Unlike CreatePaymentIntent, it does not require an order ID.
// customerID and guestToken are optional; they are added to the PI metadata when provided.
func (s *stripeClient) CreatePreOrderPaymentIntent(
	_ context.Context,
	paymentMethodID uuid.UUID,
	amount int64,
	currency string,
	customerID uuid.UUID,
	guestToken string,
) (*PaymentIntentResult, error) {
	if currency == "" {
		currency = s.currency
	}

	metadata := map[string]string{
		"stoa_mode":              "pre_order",
		"stoa_payment_method_id": paymentMethodID.String(),
	}
	if customerID != uuid.Nil {
		metadata["stoa_customer_id"] = customerID.String()
	}
	if guestToken != "" {
		metadata["stoa_guest_token"] = guestToken
	}

	params := &stripe.PaymentIntentParams{
		Amount:        stripe.Int64(amount),
		Currency:      stripe.String(strings.ToLower(currency)),
		CaptureMethod: stripe.String(string(stripe.PaymentIntentCaptureMethodManual)),
		AutomaticPaymentMethods: &stripe.PaymentIntentAutomaticPaymentMethodsParams{
			Enabled: stripe.Bool(true),
		},
		Metadata: metadata,
	}

	pi, err := s.api.PaymentIntents.New(params)
	if err != nil {
		return nil, fmt.Errorf("stripe: create pre-order payment intent: %w", err)
	}

	return &PaymentIntentResult{
		ID:             pi.ID,
		ClientSecret:   pi.ClientSecret,
		PublishableKey: s.publishableKey,
		Amount:         pi.Amount,
		Currency:       string(pi.Currency),
	}, nil
}

// RefundPaymentIntent creates a full refund for the given PaymentIntent ID.
func (s *stripeClient) RefundPaymentIntent(_ context.Context, paymentIntentID string) error {
	_, err := s.api.Refunds.New(&stripe.RefundParams{
		PaymentIntent: stripe.String(paymentIntentID),
	})
	if err != nil {
		return fmt.Errorf("stripe: refund payment intent %s: %w", paymentIntentID, err)
	}
	return nil
}

// CapturePaymentIntent captures an authorized PaymentIntent.
func (s *stripeClient) CapturePaymentIntent(_ context.Context, paymentIntentID string) error {
	_, err := s.api.PaymentIntents.Capture(paymentIntentID, nil)
	if err != nil {
		return fmt.Errorf("stripe: capture payment intent %s: %w", paymentIntentID, err)
	}
	return nil
}

// CancelPaymentIntent cancels an authorized PaymentIntent without moving any money.
func (s *stripeClient) CancelPaymentIntent(_ context.Context, paymentIntentID string) error {
	_, err := s.api.PaymentIntents.Cancel(paymentIntentID, nil)
	if err != nil {
		return fmt.Errorf("stripe: cancel payment intent %s: %w", paymentIntentID, err)
	}
	return nil
}

// DashboardURL returns the Stripe Dashboard URL for a PaymentIntent.
// It uses the publishable key prefix to determine test vs. live mode.
func (s *stripeClient) DashboardURL(paymentIntentID string) string {
	if paymentIntentID == "" {
		return ""
	}
	prefix := "https://dashboard.stripe.com"
	if strings.HasPrefix(s.publishableKey, "pk_test_") {
		prefix += "/test"
	}
	return prefix + "/payments/" + paymentIntentID
}
