package stripe

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	stripe "github.com/stripe/stripe-go/v82"
	"github.com/stoa-hq/stoa/pkg/sdk"
)

// handlePaymentIntentSucceeded processes a payment_intent.succeeded event.
// It creates a payment_transaction record and transitions the order status to "confirmed".
func handlePaymentIntentSucceeded(
	ctx context.Context,
	pi *stripe.PaymentIntent,
	db *pgxpool.Pool,
	hooks *sdk.HookRegistry,
	logger zerolog.Logger,
) {
	orderID, paymentMethodID, err := extractMetadata(pi)
	if err != nil {
		logger.Error().Err(err).Str("payment_intent_id", pi.ID).
			Msg("stripe webhook: failed to extract metadata from payment intent")
		return
	}

	txID, err := createTransaction(ctx, db, orderID, paymentMethodID, "completed", pi)
	if err != nil {
		logger.Error().Err(err).
			Str("order_id", orderID.String()).
			Msg("stripe webhook: failed to create payment transaction")
		return
	}

	if err := updateOrderStatus(ctx, db, orderID, "pending", "confirmed",
		fmt.Sprintf("Payment completed via Stripe (pi: %s)", pi.ID),
	); err != nil {
		logger.Error().Err(err).
			Str("order_id", orderID.String()).
			Msg("stripe webhook: failed to update order status")
		return
	}

	hooks.Dispatch(ctx, &sdk.HookEvent{ //nolint:errcheck // after-hook; errors must not roll back
		Name: sdk.HookAfterPaymentComplete,
		Entity: map[string]interface{}{
			"order_id":               orderID.String(),
			"payment_transaction_id": txID.String(),
			"provider_reference":     pi.ID,
			"amount":                 pi.Amount,
			"currency":               string(pi.Currency),
		},
	})

	logger.Info().
		Str("order_id", orderID.String()).
		Str("payment_intent_id", pi.ID).
		Msg("stripe webhook: order confirmed after successful payment")
}

// handlePaymentIntentFailed processes a payment_intent.payment_failed event.
func handlePaymentIntentFailed(
	ctx context.Context,
	pi *stripe.PaymentIntent,
	db *pgxpool.Pool,
	hooks *sdk.HookRegistry,
	logger zerolog.Logger,
) {
	orderID, paymentMethodID, err := extractMetadata(pi)
	if err != nil {
		logger.Error().Err(err).Str("payment_intent_id", pi.ID).
			Msg("stripe webhook: failed to extract metadata from payment intent")
		return
	}

	txID, err := createTransaction(ctx, db, orderID, paymentMethodID, "failed", pi)
	if err != nil {
		logger.Error().Err(err).
			Str("order_id", orderID.String()).
			Msg("stripe webhook: failed to create payment transaction")
		return
	}

	hooks.Dispatch(ctx, &sdk.HookEvent{ //nolint:errcheck
		Name: sdk.HookAfterPaymentFailed,
		Entity: map[string]interface{}{
			"order_id":               orderID.String(),
			"payment_transaction_id": txID.String(),
			"provider_reference":     pi.ID,
			"amount":                 pi.Amount,
			"currency":               string(pi.Currency),
		},
	})

	logger.Warn().
		Str("order_id", orderID.String()).
		Str("payment_intent_id", pi.ID).
		Msg("stripe webhook: payment failed")
}

// extractMetadata reads stoa_order_id and stoa_payment_method_id from PaymentIntent metadata.
func extractMetadata(pi *stripe.PaymentIntent) (orderID uuid.UUID, paymentMethodID uuid.UUID, err error) {
	rawOrder, ok := pi.Metadata["stoa_order_id"]
	if !ok || rawOrder == "" {
		return uuid.Nil, uuid.Nil, fmt.Errorf("missing stoa_order_id in payment intent metadata")
	}
	orderID, err = uuid.Parse(rawOrder)
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("invalid stoa_order_id %q: %w", rawOrder, err)
	}

	rawPM, ok := pi.Metadata["stoa_payment_method_id"]
	if !ok || rawPM == "" {
		return uuid.Nil, uuid.Nil, fmt.Errorf("missing stoa_payment_method_id in payment intent metadata")
	}
	paymentMethodID, err = uuid.Parse(rawPM)
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("invalid stoa_payment_method_id %q: %w", rawPM, err)
	}

	return orderID, paymentMethodID, nil
}

// createTransaction inserts a payment_transaction row and returns its ID.
func createTransaction(
	ctx context.Context,
	db *pgxpool.Pool,
	orderID uuid.UUID,
	paymentMethodID uuid.UUID,
	status string,
	pi *stripe.PaymentIntent,
) (uuid.UUID, error) {
	id := uuid.New()
	_, err := db.Exec(ctx, `
		INSERT INTO payment_transactions
			(id, order_id, payment_method_id, status, amount, currency, provider_reference, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		id,
		orderID,
		paymentMethodID,
		status,
		pi.Amount,
		strings.ToUpper(string(pi.Currency)),
		pi.ID,
		time.Now().UTC(),
	)
	if err != nil {
		return uuid.Nil, fmt.Errorf("insert payment_transaction: %w", err)
	}
	return id, nil
}

// updateOrderStatus transitions an order to toStatus and records the change
// in order_status_history. It is a no-op if the order is already at toStatus.
func updateOrderStatus(
	ctx context.Context,
	db *pgxpool.Pool,
	orderID uuid.UUID,
	fromStatus, toStatus, comment string,
) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var currentStatus string
	if err := tx.QueryRow(ctx,
		`SELECT status FROM orders WHERE id = $1 FOR UPDATE`,
		orderID,
	).Scan(&currentStatus); err != nil {
		return fmt.Errorf("fetch order status: %w", err)
	}

	// If already at target status, nothing to do.
	if currentStatus == toStatus {
		return nil
	}
	// Only transition if currently at expected fromStatus.
	if currentStatus != fromStatus {
		return fmt.Errorf("order %s: expected status %q, got %q", orderID, fromStatus, currentStatus)
	}

	if _, err := tx.Exec(ctx,
		`UPDATE orders SET status = $1, updated_at = NOW() WHERE id = $2`,
		toStatus, orderID,
	); err != nil {
		return fmt.Errorf("update order status: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO order_status_history (id, order_id, from_status, to_status, comment, created_at)
		VALUES ($1, $2, $3, $4, $5, NOW())`,
		uuid.New(), orderID, fromStatus, toStatus, comment,
	); err != nil {
		return fmt.Errorf("insert status history: %w", err)
	}

	return tx.Commit(ctx)
}

// unmarshalPaymentIntent extracts the PaymentIntent from the raw event data.
func unmarshalPaymentIntent(data json.RawMessage) (*stripe.PaymentIntent, error) {
	var pi stripe.PaymentIntent
	if err := json.Unmarshal(data, &pi); err != nil {
		return nil, fmt.Errorf("unmarshal payment intent: %w", err)
	}
	return &pi, nil
}
