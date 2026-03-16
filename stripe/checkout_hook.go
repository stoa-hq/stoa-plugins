package stripe

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/stoa-hq/stoa/pkg/sdk"
)

// registerCheckoutHooks registers both before and after checkout hooks.
// - before: validates that the Stripe PaymentIntent has succeeded.
// - after: creates a payment transaction and confirms the order for pay-first checkouts.
func registerCheckoutHooks(hooks *sdk.HookRegistry, sc *stripeClient, db *pgxpool.Pool, logger zerolog.Logger) {
	hooks.On(sdk.HookBeforeCheckout, func(ctx context.Context, event *sdk.HookEvent) error {
		return validateCheckoutPayment(ctx, event, sc, logger)
	})
	hooks.On(sdk.HookAfterCheckout, func(ctx context.Context, event *sdk.HookEvent) error {
		finalizePreOrderPayment(ctx, event, sc, db, logger)
		return nil // after-hooks must not fail the checkout
	})
}

// validateCheckoutPayment checks that a Stripe PaymentIntent has succeeded
// when the checkout uses a Stripe-backed payment method.
func validateCheckoutPayment(ctx context.Context, event *sdk.HookEvent, sc *stripeClient, logger zerolog.Logger) error {
	provider, _ := event.Metadata["provider"].(string)
	if provider != "stripe" {
		return nil
	}

	ref, _ := event.Metadata["payment_reference"].(string)
	if ref == "" {
		return fmt.Errorf("payment_reference is required for Stripe payments")
	}

	pi, err := sc.RetrievePaymentIntent(ctx, ref)
	if err != nil {
		logger.Error().Err(err).Str("payment_reference", ref).Msg("stripe: failed to retrieve payment intent")
		return fmt.Errorf("failed to verify payment")
	}

	if string(pi.Status) != "succeeded" {
		logger.Warn().
			Str("payment_intent_id", pi.ID).
			Str("status", string(pi.Status)).
			Msg("stripe: payment intent not succeeded")
		return fmt.Errorf("payment has not been completed (status: %s)", pi.Status)
	}

	return nil
}

// finalizePreOrderPayment creates a payment transaction and transitions the
// order to "confirmed" after a successful pay-first checkout.
func finalizePreOrderPayment(ctx context.Context, event *sdk.HookEvent, sc *stripeClient, db *pgxpool.Pool, logger zerolog.Logger) {
	provider, _ := event.Metadata["provider"].(string)
	if provider != "stripe" {
		return
	}

	ref, _ := event.Metadata["payment_reference"].(string)
	if ref == "" {
		return
	}

	// Retrieve the PaymentIntent to get amount/currency.
	pi, err := sc.RetrievePaymentIntent(ctx, ref)
	if err != nil {
		logger.Error().Err(err).Str("payment_reference", ref).
			Msg("stripe: after_checkout: failed to retrieve payment intent")
		return
	}

	// Look up the order by payment_reference to get its ID and payment_method_id.
	var orderID, paymentMethodID uuid.UUID
	err = db.QueryRow(ctx,
		`SELECT id, payment_method_id FROM orders WHERE payment_reference = $1`,
		ref,
	).Scan(&orderID, &paymentMethodID)
	if err != nil {
		logger.Error().Err(err).Str("payment_reference", ref).
			Msg("stripe: after_checkout: failed to find order by payment_reference")
		return
	}

	// Create a completed payment transaction directly.
	_, err = db.Exec(ctx, `
		INSERT INTO payment_transactions
			(id, order_id, payment_method_id, status, amount, currency, provider_reference, created_at)
		VALUES ($1, $2, $3, 'completed', $4, $5, $6, NOW())
		ON CONFLICT (provider_reference) WHERE provider_reference IS NOT NULL
		DO UPDATE SET status = 'completed'`,
		uuid.New(), orderID, paymentMethodID, pi.Amount, string(pi.Currency), ref,
	)
	if err != nil {
		logger.Error().Err(err).
			Str("order_id", orderID.String()).
			Str("payment_intent_id", ref).
			Msg("stripe: after_checkout: failed to insert transaction")
	}

	// Transition order: pending → confirmed.
	if err := updateOrderStatus(ctx, db, orderID, "pending", "confirmed",
		fmt.Sprintf("Payment completed via Stripe (pi: %s)", ref),
	); err != nil {
		logger.Error().Err(err).
			Str("order_id", orderID.String()).
			Msg("stripe: after_checkout: failed to confirm order")
		return
	}

	logger.Info().
		Str("order_id", orderID.String()).
		Str("payment_intent_id", ref).
		Msg("stripe: after_checkout: order confirmed with pre-order payment")
}
