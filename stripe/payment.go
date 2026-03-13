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

// CreatePaymentIntent creates a Stripe PaymentIntent for the given order.
// The orderID and paymentMethodID are stored in the PaymentIntent metadata so
// they can be recovered when Stripe fires the webhook event.
func (s *stripeClient) CreatePaymentIntent(
	_ context.Context,
	orderID uuid.UUID,
	paymentMethodID uuid.UUID,
	amount int64,
	currency string,
) (*PaymentIntentResult, error) {
	if currency == "" {
		currency = s.currency
	}

	params := &stripe.PaymentIntentParams{
		Amount:   stripe.Int64(amount),
		Currency: stripe.String(strings.ToLower(currency)),
		AutomaticPaymentMethods: &stripe.PaymentIntentAutomaticPaymentMethodsParams{
			Enabled: stripe.Bool(true),
		},
		Metadata: map[string]string{
			"stoa_order_id":          orderID.String(),
			"stoa_payment_method_id": paymentMethodID.String(),
		},
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
