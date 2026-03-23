package stripe

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const paymentLinkExpiry = 30 * time.Minute

// PaymentLink represents an ephemeral payment link created by an MCP agent.
// It stores the full checkout context so the storefront payment page can
// complete the order without the customer re-entering data.
type PaymentLink struct {
	ID               uuid.UUID       `json:"id"`
	Token            string          `json:"token"`
	CartID           uuid.UUID       `json:"cart_id"`
	CustomerID       *uuid.UUID      `json:"customer_id,omitempty"`
	GuestToken       string          `json:"guest_token,omitempty"`
	PaymentIntentID  string          `json:"payment_intent_id"`
	ClientSecret     string          `json:"client_secret"`
	PublishableKey   string          `json:"publishable_key"`
	PaymentMethodID  uuid.UUID       `json:"payment_method_id"`
	ShippingMethodID uuid.UUID       `json:"shipping_method_id"`
	Amount           int64           `json:"amount"`
	Currency         string          `json:"currency"`
	Email            string          `json:"email,omitempty"`
	ShippingAddress  json.RawMessage `json:"shipping_address"`
	BillingAddress   json.RawMessage `json:"billing_address,omitempty"`
	Status           string          `json:"status"`
	ExpiresAt        time.Time       `json:"expires_at"`
	CreatedAt        time.Time       `json:"created_at"`
}

// generateToken creates a URL-safe random token with 256 bits of entropy.
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ensurePaymentLinksTable creates the stripe_payment_links table if it does
// not exist. Called once during plugin Init.
func ensurePaymentLinksTable(ctx context.Context, db *pgxpool.Pool) error {
	_, err := db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS stripe_payment_links (
			id                 UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			token              VARCHAR(64)  NOT NULL UNIQUE,
			cart_id            UUID         NOT NULL,
			customer_id        UUID,
			guest_token        VARCHAR(255),
			payment_intent_id  VARCHAR(255) NOT NULL,
			client_secret      VARCHAR(512) NOT NULL,
			publishable_key    VARCHAR(255) NOT NULL,
			payment_method_id  UUID         NOT NULL,
			shipping_method_id UUID         NOT NULL,
			amount             BIGINT       NOT NULL,
			currency           VARCHAR(3)   NOT NULL,
			email              VARCHAR(255),
			shipping_address   JSONB        NOT NULL,
			billing_address    JSONB,
			status             VARCHAR(20)  NOT NULL DEFAULT 'pending',
			expires_at         TIMESTAMPTZ  NOT NULL,
			created_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW()
		)`)
	if err != nil {
		return fmt.Errorf("creating stripe_payment_links table: %w", err)
	}
	_, err = db.Exec(ctx, `
		CREATE INDEX IF NOT EXISTS idx_stripe_payment_links_token
		ON stripe_payment_links(token)`)
	if err != nil {
		return fmt.Errorf("creating stripe_payment_links index: %w", err)
	}
	return nil
}

// insertPaymentLink stores a new payment link in the database.
func insertPaymentLink(ctx context.Context, db *pgxpool.Pool, link *PaymentLink) error {
	_, err := db.Exec(ctx, `
		INSERT INTO stripe_payment_links
			(id, token, cart_id, customer_id, guest_token, payment_intent_id,
			 client_secret, publishable_key, payment_method_id, shipping_method_id,
			 amount, currency, email, shipping_address, billing_address, status, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)`,
		link.ID, link.Token, link.CartID, link.CustomerID, link.GuestToken,
		link.PaymentIntentID, link.ClientSecret, link.PublishableKey,
		link.PaymentMethodID, link.ShippingMethodID,
		link.Amount, link.Currency, link.Email,
		link.ShippingAddress, link.BillingAddress,
		link.Status, link.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("inserting payment link: %w", err)
	}
	return nil
}

// getPaymentLinkByToken retrieves a payment link by its token.
// Returns nil if not found.
func getPaymentLinkByToken(ctx context.Context, db *pgxpool.Pool, token string) (*PaymentLink, error) {
	var link PaymentLink
	err := db.QueryRow(ctx, `
		SELECT id, token, cart_id, customer_id, guest_token, payment_intent_id,
		       client_secret, publishable_key, payment_method_id, shipping_method_id,
		       amount, currency, email, shipping_address, billing_address, status,
		       expires_at, created_at
		FROM stripe_payment_links
		WHERE token = $1`, token).Scan(
		&link.ID, &link.Token, &link.CartID, &link.CustomerID, &link.GuestToken,
		&link.PaymentIntentID, &link.ClientSecret, &link.PublishableKey,
		&link.PaymentMethodID, &link.ShippingMethodID,
		&link.Amount, &link.Currency, &link.Email,
		&link.ShippingAddress, &link.BillingAddress,
		&link.Status, &link.ExpiresAt, &link.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &link, nil
}

// getPaymentLinkByPaymentIntentID retrieves a payment link by its Stripe PaymentIntent ID.
// Returns nil, nil if not found.
func getPaymentLinkByPaymentIntentID(ctx context.Context, db *pgxpool.Pool, paymentIntentID string) (*PaymentLink, error) {
	var link PaymentLink
	err := db.QueryRow(ctx, `
		SELECT id, token, cart_id, customer_id, guest_token, payment_intent_id,
		       client_secret, publishable_key, payment_method_id, shipping_method_id,
		       amount, currency, email, shipping_address, billing_address, status,
		       expires_at, created_at
		FROM stripe_payment_links
		WHERE payment_intent_id = $1
		LIMIT 1`, paymentIntentID).Scan(
		&link.ID, &link.Token, &link.CartID, &link.CustomerID, &link.GuestToken,
		&link.PaymentIntentID, &link.ClientSecret, &link.PublishableKey,
		&link.PaymentMethodID, &link.ShippingMethodID,
		&link.Amount, &link.Currency, &link.Email,
		&link.ShippingAddress, &link.BillingAddress,
		&link.Status, &link.ExpiresAt, &link.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &link, nil
}

// completePaymentLink atomically marks a payment link as completed.
// Returns false if the link was already completed or expired.
// This is used as a distributed lock — only the first caller gets true.
func completePaymentLink(ctx context.Context, db *pgxpool.Pool, token string) (bool, error) {
	tag, err := db.Exec(ctx, `
		UPDATE stripe_payment_links
		SET status = 'completed'
		WHERE token = $1 AND status = 'pending' AND expires_at > NOW()`,
		token)
	if err != nil {
		return false, fmt.Errorf("completing payment link: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// revertPaymentLink sets a completed payment link back to pending.
// Used when checkout fails after the link was atomically claimed.
func revertPaymentLink(ctx context.Context, db *pgxpool.Pool, token string) {
	_, err := db.Exec(ctx, `
		UPDATE stripe_payment_links
		SET status = 'pending'
		WHERE token = $1 AND status = 'completed'`,
		token)
	if err != nil {
		// Best-effort — the link stays completed, which is safe (prevents retry).
		// Manual intervention needed to retry.
		_ = err
	}
}
