package stripe

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	stripelib "github.com/stripe/stripe-go/v82"
	"github.com/stoa-hq/stoa/pkg/sdk"
)

// registerCheckoutHooks registers before/after checkout hooks and the order-update hook
// for deferred capture.
// - before: validates that the Stripe PaymentIntent is authorized (requires_capture or succeeded).
// - after: captures payment (if CaptureOn == "confirmed") and confirms the order.
// - after_failed: cancels the PaymentIntent without moving money.
// - order_update: captures payment when the order reaches the configured CaptureOn status.
func registerCheckoutHooks(hooks *sdk.HookRegistry, sc *stripeClient, db *pgxpool.Pool, cfg Config, logger zerolog.Logger) {
	hooks.On(sdk.HookBeforeCheckout, func(ctx context.Context, event *sdk.HookEvent) error {
		return validateCheckoutPayment(ctx, event, sc, logger)
	})
	hooks.On(sdk.HookAfterCheckout, func(ctx context.Context, event *sdk.HookEvent) error {
		finalizePreOrderPayment(ctx, event, sc, db, cfg.CaptureOn, logger)
		return nil // after-hooks must not fail the checkout
	})
	hooks.On(sdk.HookAfterCheckoutFailed, func(ctx context.Context, event *sdk.HookEvent) error {
		handleFailedCheckoutPayment(ctx, event, sc, logger)
		return nil // nicht-fatal
	})
	hooks.On(sdk.HookAfterOrderUpdate, func(ctx context.Context, event *sdk.HookEvent) error {
		if cfg.CaptureOn == "confirmed" {
			return nil // handled inside AfterCheckout
		}
		captureOnStatusChange(ctx, event, sc, db, cfg.CaptureOn, logger)
		return nil
	})
}

// handleFailedCheckoutPayment cancels the Stripe PaymentIntent when checkout fails
// (e.g. insufficient stock). Since capture_method=manual, no money was moved.
func handleFailedCheckoutPayment(ctx context.Context, event *sdk.HookEvent, sc *stripeClient, logger zerolog.Logger) {
	provider, _ := event.Metadata["provider"].(string)
	if provider != "stripe" {
		return
	}
	ref, _ := event.Metadata["payment_reference"].(string)
	if ref == "" {
		return
	}
	if err := sc.CancelPaymentIntent(ctx, ref); err != nil {
		logger.Error().Err(err).Str("payment_intent_id", ref).Msg("stripe: failed to cancel payment intent after checkout failure")
		return
	}
	logger.Info().Str("payment_intent_id", ref).Msg("stripe: payment intent cancelled after checkout failure")
}

// validateCheckoutPayment checks that a Stripe PaymentIntent has been authorized
// (requires_capture) or already succeeded when the checkout uses Stripe.
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

	if string(pi.Status) != "succeeded" && string(pi.Status) != "requires_capture" {
		logger.Warn().
			Str("payment_intent_id", pi.ID).
			Str("status", string(pi.Status)).
			Msg("stripe: payment intent not authorized")
		return fmt.Errorf("payment has not been authorized (status: %s)", pi.Status)
	}

	return nil
}

// finalizePreOrderPayment creates a payment transaction and transitions the
// order to "confirmed" after a successful pay-first checkout.
// If captureOn == "confirmed", the PaymentIntent is captured immediately.
// Otherwise the transaction is created as "pending" and capture happens later.
func finalizePreOrderPayment(ctx context.Context, event *sdk.HookEvent, sc *stripeClient, db *pgxpool.Pool, captureOn string, logger zerolog.Logger) {
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

	// Look up the order by payment_reference to get its ID, payment_method_id, and billing_address.
	var orderID, paymentMethodID uuid.UUID
	var billingAddress []byte
	err = db.QueryRow(ctx,
		`SELECT id, payment_method_id, billing_address FROM orders WHERE payment_reference = $1`,
		ref,
	).Scan(&orderID, &paymentMethodID, &billingAddress)
	if err != nil {
		logger.Error().Err(err).Str("payment_reference", ref).
			Msg("stripe: after_checkout: failed to find order by payment_reference")
		return
	}

	// Set receipt_email on the PaymentIntent now that the order (with billing address) exists.
	if email := extractReceiptEmail(billingAddress); email != "" {
		if _, updateErr := sc.api.PaymentIntents.Update(ref, &stripelib.PaymentIntentParams{
			ReceiptEmail: stripelib.String(email),
		}); updateErr != nil {
			logger.Warn().Err(updateErr).Str("payment_intent_id", ref).Msg("stripe: after_checkout: failed to set receipt email")
		}
	}

	// Capture immediately when CaptureOn == "confirmed".
	txStatus := "pending"
	if captureOn == "confirmed" {
		if err := sc.CapturePaymentIntent(ctx, ref); err != nil {
			logger.Error().Err(err).
				Str("order_id", orderID.String()).
				Str("payment_intent_id", ref).
				Msg("stripe: after_checkout: failed to capture payment intent")
			return
		}
		txStatus = "completed"
	}

	// Upsert payment transaction.
	_, err = db.Exec(ctx, `
		INSERT INTO payment_transactions
			(id, order_id, payment_method_id, status, amount, currency, provider_reference, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
		ON CONFLICT (provider_reference) WHERE provider_reference IS NOT NULL
		DO UPDATE SET status = $4`,
		uuid.New(), orderID, paymentMethodID, txStatus, pi.Amount, string(pi.Currency), ref,
	)
	if err != nil {
		logger.Error().Err(err).
			Str("order_id", orderID.String()).
			Str("payment_intent_id", ref).
			Msg("stripe: after_checkout: failed to upsert transaction")
	}

	// Transition order: pending → confirmed (always, regardless of CaptureOn).
	if err := updateOrderStatus(ctx, db, orderID, "pending", "confirmed",
		fmt.Sprintf("Payment authorized via Stripe (pi: %s)", ref),
	); err != nil {
		logger.Error().Err(err).
			Str("order_id", orderID.String()).
			Msg("stripe: after_checkout: failed to confirm order")
		return
	}

	logger.Info().
		Str("order_id", orderID.String()).
		Str("payment_intent_id", ref).
		Str("tx_status", txStatus).
		Msg("stripe: after_checkout: order confirmed after pre-order payment")
}

// captureOnStatusChange captures the PaymentIntent and marks the transaction
// as completed when the order reaches the configured CaptureOn status.
func captureOnStatusChange(ctx context.Context, event *sdk.HookEvent, sc *stripeClient, db *pgxpool.Pool, captureOn string, logger zerolog.Logger) {
	toStatus, _ := event.Changes["to_status"].(string)
	if toStatus != captureOn {
		return
	}

	ref, _ := event.Changes["payment_reference"].(string)
	if ref == "" {
		return
	}

	if err := sc.CapturePaymentIntent(ctx, ref); err != nil {
		logger.Error().Err(err).
			Str("payment_intent_id", ref).
			Str("capture_on", captureOn).
			Msg("stripe: failed to capture payment intent on status change")
		return
	}

	if db == nil {
		logger.Error().Str("payment_intent_id", ref).Msg("stripe: db is nil, cannot update transaction status after capture")
		return
	}

	_, err := db.Exec(ctx,
		`UPDATE payment_transactions SET status = 'completed' WHERE provider_reference = $1`, ref)
	if err != nil {
		logger.Error().Err(err).
			Str("payment_intent_id", ref).
			Msg("stripe: failed to update transaction status after capture")
		return
	}

	logger.Info().
		Str("payment_intent_id", ref).
		Str("to_status", toStatus).
		Msg("stripe: payment intent captured on status change")
}
