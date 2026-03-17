package stripe

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
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
		handleFailedCheckoutPayment(ctx, event, sc, db, logger)
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
// It also creates a "cancelled" payment transaction so the Admin Panel shows the
// link to the Stripe transaction for manual verification.
func handleFailedCheckoutPayment(ctx context.Context, event *sdk.HookEvent, sc *stripeClient, db *pgxpool.Pool, logger zerolog.Logger) {
	provider, _ := event.Metadata["provider"].(string)
	if provider != "stripe" {
		return
	}
	ref, _ := event.Metadata["payment_reference"].(string)
	if ref == "" {
		return
	}

	// Retrieve the PI before cancellation to get amount/currency.
	pi, err := sc.RetrievePaymentIntent(ctx, ref)
	if err != nil {
		logger.Error().Err(err).Str("payment_intent_id", ref).Msg("stripe: failed to retrieve payment intent before cancellation")
	}

	if err := sc.CancelPaymentIntent(ctx, ref); err != nil {
		logger.Error().Err(err).Str("payment_intent_id", ref).Msg("stripe: failed to cancel payment intent after checkout failure")
		return
	}
	logger.Info().Str("payment_intent_id", ref).Msg("stripe: payment intent cancelled after checkout failure")

	// Insert a "cancelled" transaction so the Admin Panel has a link to the
	// Stripe transaction for verification.
	if db != nil {
		var orderID, paymentMethodID uuid.UUID
		qErr := db.QueryRow(ctx,
			`SELECT id, payment_method_id FROM orders WHERE payment_reference = $1`, ref,
		).Scan(&orderID, &paymentMethodID)
		if qErr != nil {
			logger.Error().Err(qErr).Str("payment_reference", ref).
				Msg("stripe: after_checkout_failed: could not find order for cancelled transaction")
			return
		}

		var amount int64
		var currency string
		if pi != nil {
			amount = pi.Amount
			currency = string(pi.Currency)
		}

		if txErr := insertCancelledTransaction(ctx, db, orderID, paymentMethodID, ref, amount, currency); txErr != nil {
			logger.Error().Err(txErr).
				Str("order_id", orderID.String()).
				Str("payment_intent_id", ref).
				Msg("stripe: after_checkout_failed: failed to insert cancelled transaction")
		}
	}
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

	// Look up the order by payment_reference to get its ID, payment_method_id,
	// customer_id, guest_token, and order_number.
	var orderID, paymentMethodID uuid.UUID
	var customerID *uuid.UUID
	var guestTokenVal *string
	var orderNumberVal string
	err = db.QueryRow(ctx,
		`SELECT id, payment_method_id, customer_id, guest_token, order_number FROM orders WHERE payment_reference = $1`,
		ref,
	).Scan(&orderID, &paymentMethodID, &customerID, &guestTokenVal, &orderNumberVal)
	if err != nil {
		logger.Error().Err(err).Str("payment_reference", ref).
			Msg("stripe: after_checkout: failed to find order by payment_reference")
		return
	}

	// Enrich PaymentIntent metadata with order data (order was created after PI).
	captureMeta := map[string]string{
		"stoa_order_id":     orderID.String(),
		"stoa_order_number": orderNumberVal,
	}
	if customerID != nil {
		captureMeta["stoa_customer_id"] = customerID.String()
	}
	if guestTokenVal != nil && *guestTokenVal != "" {
		captureMeta["stoa_guest_token"] = *guestTokenVal
	}
	if err := sc.UpdatePaymentIntentMetadata(ctx, ref, captureMeta); err != nil {
		logger.Error().Err(err).
			Str("order_id", orderID.String()).
			Str("payment_intent_id", ref).
			Msg("stripe: after_checkout: failed to update payment intent metadata")
		// Non-fatal: continue with capture even if metadata update fails.
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

	// Enrich PaymentIntent metadata with order data before capture.
	if db != nil {
		var orderID uuid.UUID
		var customerID *uuid.UUID
		var guestTokenVal *string
		var orderNumberVal string
		qErr := db.QueryRow(ctx,
			`SELECT id, customer_id, guest_token, order_number FROM orders WHERE payment_reference = $1`,
			ref,
		).Scan(&orderID, &customerID, &guestTokenVal, &orderNumberVal)
		if qErr == nil {
			captureMeta := map[string]string{
				"stoa_order_id":     orderID.String(),
				"stoa_order_number": orderNumberVal,
			}
			if customerID != nil {
				captureMeta["stoa_customer_id"] = customerID.String()
			}
			if guestTokenVal != nil && *guestTokenVal != "" {
				captureMeta["stoa_guest_token"] = *guestTokenVal
			}
			if uErr := sc.UpdatePaymentIntentMetadata(ctx, ref, captureMeta); uErr != nil {
				logger.Error().Err(uErr).Str("payment_intent_id", ref).
					Msg("stripe: failed to update payment intent metadata before capture")
			}
		} else {
			logger.Error().Err(qErr).Str("payment_reference", ref).
				Msg("stripe: failed to find order for metadata enrichment before capture")
		}
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
