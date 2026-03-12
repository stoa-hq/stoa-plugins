package stripe

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/stoa-hq/stoa/pkg/sdk"
	stripewebhook "github.com/stripe/stripe-go/v82/webhook"
)

const maxWebhookBodyBytes = 65536

// mountRoutes registers all HTTP routes for the Stripe plugin.
func mountRoutes(
	router chi.Router,
	sc *stripeClient,
	db *pgxpool.Pool,
	hooks *sdk.HookRegistry,
	webhookSecret string,
	logger zerolog.Logger,
) {
	// Store-facing route: agents / frontend use this to create a PaymentIntent.
	router.Post("/api/v1/store/stripe/payment-intent",
		paymentIntentHandler(sc, db, logger))

	// Stripe webhook receiver.
	router.Post("/plugins/stripe/webhook",
		webhookHandler(db, hooks, webhookSecret, logger))

	// Admin health check.
	router.Get("/plugins/stripe/health",
		healthHandler(sc, logger))
}

// paymentIntentRequest is the body expected by POST /api/v1/store/stripe/payment-intent.
type paymentIntentRequest struct {
	OrderID         string `json:"order_id"`
	PaymentMethodID string `json:"payment_method_id"`
}

// paymentIntentHandler creates a Stripe PaymentIntent for an existing (pending) order.
// It looks up the order total and currency from the database, then calls Stripe.
func paymentIntentHandler(sc *stripeClient, db *pgxpool.Pool, logger zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req paymentIntentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		orderID, err := uuid.Parse(req.OrderID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid order_id")
			return
		}
		paymentMethodID, err := uuid.Parse(req.PaymentMethodID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid payment_method_id")
			return
		}

		var (
			total    int64
			currency string
			status   string
		)
		row := db.QueryRow(r.Context(),
			`SELECT total, currency, status FROM orders WHERE id = $1`, orderID)
		if err := row.Scan(&total, &currency, &status); err != nil {
			logger.Error().Err(err).Str("order_id", orderID.String()).Msg("stripe: fetch order")
			writeError(w, http.StatusNotFound, "order not found")
			return
		}
		if status != "pending" {
			writeError(w, http.StatusUnprocessableEntity,
				"payment intent can only be created for pending orders")
			return
		}

		result, err := sc.CreatePaymentIntent(r.Context(), orderID, paymentMethodID, total, currency)
		if err != nil {
			logger.Error().Err(err).Str("order_id", orderID.String()).Msg("stripe: create payment intent")
			writeError(w, http.StatusBadGateway, "failed to create payment intent")
			return
		}

		writeJSON(w, http.StatusCreated, map[string]interface{}{"data": result})
	}
}

// webhookHandler verifies the Stripe signature and dispatches the event.
func webhookHandler(
	db *pgxpool.Pool,
	hooks *sdk.HookRegistry,
	webhookSecret string,
	logger zerolog.Logger,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBodyBytes))
		if err != nil {
			writeError(w, http.StatusBadRequest, "cannot read body")
			return
		}

		sigHeader := r.Header.Get("Stripe-Signature")
		event, err := stripewebhook.ConstructEventWithOptions(body, sigHeader, webhookSecret,
			stripewebhook.ConstructEventOptions{IgnoreAPIVersionMismatch: true},
		)
		if err != nil {
			logger.Warn().Err(err).Msg("stripe webhook: signature verification failed")
			writeError(w, http.StatusUnauthorized, "invalid signature")
			return
		}

		ctx := r.Context()

		switch event.Type {
		case "payment_intent.succeeded":
			pi, err := unmarshalPaymentIntent(event.Data.Raw)
			if err != nil {
				logger.Error().Err(err).Msg("stripe webhook: unmarshal payment intent")
				writeError(w, http.StatusBadRequest, "invalid event data")
				return
			}
			go handlePaymentIntentSucceeded(ctx, pi, db, hooks, logger)

		case "payment_intent.payment_failed":
			pi, err := unmarshalPaymentIntent(event.Data.Raw)
			if err != nil {
				logger.Error().Err(err).Msg("stripe webhook: unmarshal payment intent")
				writeError(w, http.StatusBadRequest, "invalid event data")
				return
			}
			go handlePaymentIntentFailed(ctx, pi, db, hooks, logger)

		default:
			// Acknowledged but not handled.
			logger.Debug().Str("event_type", string(event.Type)).Msg("stripe webhook: unhandled event")
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// healthHandler returns the plugin status.
func healthHandler(sc *stripeClient, logger zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status":          "ok",
			"plugin":          pluginName,
			"version":         pluginVersion,
			"publishable_key": sc.publishableKey,
			"checked_at":      time.Now().UTC().Format(time.RFC3339),
		})
		logger.Debug().Msg("stripe health check")
	}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func writeError(w http.ResponseWriter, status int, detail string) {
	writeJSON(w, status, map[string]interface{}{
		"errors": []map[string]string{{"detail": detail}},
	})
}
