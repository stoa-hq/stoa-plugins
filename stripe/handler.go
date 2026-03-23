package stripe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/stoa-hq/stoa/pkg/sdk"
	stripewebhook "github.com/stripe/stripe-go/v82/webhook"
)

const (
	maxWebhookBodyBytes       = 65536
	maxPaymentIntentBodyBytes = 4096
	webhookProcessingTimeout  = 30 * time.Second
)

// mountRoutes registers all HTTP routes for the Stripe plugin.
func mountRoutes(
	router chi.Router,
	sc *stripeClient,
	db *pgxpool.Pool,
	hooks *sdk.HookRegistry,
	authHelper *sdk.AuthHelper,
	checkoutFn sdk.CheckoutFn,
	secureCookie bool,
	cfg Config,
	logger zerolog.Logger,
) {
	// Store-facing route: supports both authenticated and guest checkout.
	router.Route("/api/v1/store/stripe", func(r chi.Router) {
		r.Use(authHelper.OptionalAuth)
		r.Post("/payment-intent",
			paymentIntentHandler(sc, db, authHelper, logger))
		r.Post("/payment-link",
			paymentLinkCreateHandler(sc, db, authHelper, cfg, logger))
		r.Get("/payment-link/{token}",
			paymentLinkGetHandler(db, logger))
		r.Post("/payment-link/{token}/complete",
			paymentLinkCompleteHandler(sc, db, checkoutFn, secureCookie, logger))
		r.Get("/payment-status/{paymentIntentID}",
			paymentStatusHandler(sc, db, authHelper, logger))
	})

	// Stripe webhook receiver — no auth; verified by Stripe signature.
	router.Post("/plugins/stripe/webhook",
		webhookHandler(db, hooks, cfg.WebhookSecret, logger))

	// Payment page — standalone HTML served by the plugin (no core dependency).
	router.Get("/plugins/stripe/pay/{token}",
		paymentPageHandler())

	// Health check — requires authentication.
	router.Group(func(r chi.Router) {
		r.Use(authHelper.Required)
		r.Get("/plugins/stripe/health",
			healthHandler(sc, logger))
	})
}

// paymentIntentRequest is the body expected by POST /api/v1/store/stripe/payment-intent.
// Either order_id (post-order) or amount+currency (pre-order) must be provided.
type paymentIntentRequest struct {
	OrderID         string `json:"order_id,omitempty"`
	PaymentMethodID string `json:"payment_method_id"`
	GuestToken      string `json:"guest_token,omitempty"`
	Amount          int64  `json:"amount,omitempty"`
	Currency        string `json:"currency,omitempty"`
	Email           string `json:"email,omitempty"`
}

// paymentIntentHandler creates a Stripe PaymentIntent.
// Two modes are supported:
//   - Post-order: order_id is provided → fetches order from DB, verifies ownership.
//   - Pre-order: amount + currency are provided, order_id is empty → creates PI without an order.
func paymentIntentHandler(sc *stripeClient, db *pgxpool.Pool, authHelper *sdk.AuthHelper, logger zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req paymentIntentRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, maxPaymentIntentBodyBytes)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		paymentMethodID, err := uuid.Parse(req.PaymentMethodID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid payment_method_id")
			return
		}

		// Pre-order mode: create PI without an existing order.
		if req.OrderID == "" {
			if req.Amount <= 0 {
				writeError(w, http.StatusBadRequest, "amount is required for pre-order payment intents")
				return
			}
			if req.Currency == "" {
				writeError(w, http.StatusBadRequest, "currency is required for pre-order payment intents")
				return
			}

			customerID := authHelper.UserID(r.Context())
			result, err := sc.CreatePreOrderPaymentIntent(r.Context(), paymentMethodID, req.Amount, req.Currency, customerID, req.GuestToken, req.Email)
			if err != nil {
				logger.Error().Err(err).Msg("stripe: create pre-order payment intent")
				writeError(w, http.StatusBadGateway, "failed to create payment intent")
				return
			}

			writeJSON(w, http.StatusCreated, map[string]interface{}{"data": result})
			return
		}

		// Post-order mode: order_id provided.
		orderID, err := uuid.Parse(req.OrderID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid order_id")
			return
		}

		userID := authHelper.UserID(r.Context())

		var (
			total          int64
			currency       string
			status         string
			orderNumber    string
			billingAddress []byte
			guestToken     *string
		)

		// Authenticated users: ownership check via customer_id.
		// Guests: ownership check via guest_token.
		var query string
		var args []interface{}
		if userID != uuid.Nil {
			query = `SELECT total, currency, status, order_number, billing_address FROM orders WHERE id = $1 AND customer_id = $2`
			args = []interface{}{orderID, userID}
			if err := db.QueryRow(r.Context(), query, args...).Scan(&total, &currency, &status, &orderNumber, &billingAddress); err != nil {
				logger.Error().Err(err).Str("order_id", orderID.String()).Msg("stripe: fetch order")
				writeError(w, http.StatusNotFound, "order not found")
				return
			}
		} else {
			if req.GuestToken == "" {
				writeError(w, http.StatusUnauthorized, "authentication or guest token required")
				return
			}
			query = `SELECT total, currency, status, order_number, billing_address, guest_token FROM orders WHERE id = $1 AND guest_token = $2 AND customer_id IS NULL`
			args = []interface{}{orderID, req.GuestToken}
			if err := db.QueryRow(r.Context(), query, args...).Scan(&total, &currency, &status, &orderNumber, &billingAddress, &guestToken); err != nil {
				logger.Error().Err(err).Str("order_id", orderID.String()).Msg("stripe: fetch order")
				writeError(w, http.StatusNotFound, "order not found")
				return
			}
		}

		if status != "pending" {
			writeError(w, http.StatusUnprocessableEntity,
				"payment intent can only be created for pending orders")
			return
		}
		if total <= 0 {
			writeError(w, http.StatusUnprocessableEntity, "order total must be positive")
			return
		}

		guestTokenValue := ""
		if guestToken != nil {
			guestTokenValue = *guestToken
		}

		oc := OrderContext{
			OrderNumber:  orderNumber,
			ReceiptEmail: extractReceiptEmail(billingAddress),
			CustomerID:   userID,
			GuestToken:   guestTokenValue,
		}

		result, err := sc.CreatePaymentIntent(r.Context(), orderID, paymentMethodID, total, currency, oc)
		if err != nil {
			logger.Error().Err(err).Str("order_id", orderID.String()).Msg("stripe: create payment intent")
			writeError(w, http.StatusBadGateway, "failed to create payment intent")
			return
		}

		// Insert a "pending" transaction so it is immediately visible in the
		// Admin Panel. The webhook will later update the status to
		// "completed" or "failed".
		if err := insertPendingTransaction(r.Context(), db, orderID, paymentMethodID, result.ID, result.Amount, result.Currency); err != nil {
			logger.Error().Err(err).
				Str("order_id", orderID.String()).
				Str("payment_intent_id", result.ID).
				Msg("stripe: insert pending transaction")
			// Non-fatal — the payment intent was already created.
		}

		writeJSON(w, http.StatusCreated, map[string]interface{}{"data": result})
	}
}

// paymentLinkCreateRequest is the body expected by POST /api/v1/store/stripe/payment-link.
type paymentLinkCreateRequest struct {
	CartID           string          `json:"cart_id"`
	PaymentMethodID  string          `json:"payment_method_id"`
	ShippingMethodID string          `json:"shipping_method_id"`
	ShippingAddress  json.RawMessage `json:"shipping_address"`
	BillingAddress   json.RawMessage `json:"billing_address,omitempty"`
	Email            string          `json:"email,omitempty"`
}

// paymentPageHandler serves the standalone payment HTML page.
// The token is in the URL path; the page uses it client-side to fetch payment link data.
// A per-request nonce is injected into the CSP header and all <script>/<style> tags
// to prevent inline script/style injection attacks.
func paymentPageHandler() http.HandlerFunc {
	pageBytes, _ := assetsFS.ReadFile("frontend/dist/pay.html")
	return func(w http.ResponseWriter, r *http.Request) {
		nonce := sdk.GenerateNonce()
		policy := "default-src 'self'; " +
			"script-src 'nonce-" + nonce + "' https://js.stripe.com; " +
			"style-src 'nonce-" + nonce + "'; " +
			"frame-src https://js.stripe.com; " +
			"connect-src 'self' https://api.stripe.com"
		w.Header().Set("Content-Security-Policy", policy)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(sdk.InjectNonce(pageBytes, nonce)) //nolint:errcheck
	}
}

// paymentLinkCreateHandler creates an ephemeral payment link for a cart.
// POST /api/v1/store/stripe/payment-link
func paymentLinkCreateHandler(sc *stripeClient, db *pgxpool.Pool, authHelper *sdk.AuthHelper, cfg Config, logger zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req paymentLinkCreateRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, maxPaymentIntentBodyBytes)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		cartID, err := uuid.Parse(req.CartID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid cart_id")
			return
		}
		paymentMethodID, err := uuid.Parse(req.PaymentMethodID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid payment_method_id")
			return
		}
		shippingMethodID, err := uuid.Parse(req.ShippingMethodID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid shipping_method_id")
			return
		}
		if len(req.ShippingAddress) == 0 {
			writeError(w, http.StatusBadRequest, "shipping_address is required")
			return
		}

		amount, err := calculateCartTotal(r.Context(), db, cartID, shippingMethodID)
		if err != nil {
			logger.Error().Err(err).Msg("stripe: calculate cart total for payment link")
			writeError(w, http.StatusUnprocessableEntity, "failed to calculate cart total")
			return
		}

		customerID := authHelper.UserID(r.Context())

		result, err := sc.CreatePreOrderPaymentIntent(r.Context(), paymentMethodID, amount, cfg.Currency, customerID, "", req.Email)
		if err != nil {
			logger.Error().Err(err).Msg("stripe: create pre-order payment intent for payment link")
			writeError(w, http.StatusBadGateway, "failed to create payment intent")
			return
		}

		token, err := generateToken()
		if err != nil {
			logger.Error().Err(err).Msg("stripe: generate payment link token")
			writeError(w, http.StatusInternalServerError, "failed to generate payment link")
			return
		}

		billingAddress := req.BillingAddress
		if len(billingAddress) == 0 {
			billingAddress = req.ShippingAddress
		}

		var customerIDPtr *uuid.UUID
		if customerID != uuid.Nil {
			customerIDPtr = &customerID
		}

		link := &PaymentLink{
			ID:               uuid.New(),
			Token:            token,
			CartID:           cartID,
			CustomerID:       customerIDPtr,
			PaymentIntentID:  result.ID,
			ClientSecret:     result.ClientSecret,
			PublishableKey:   result.PublishableKey,
			PaymentMethodID:  paymentMethodID,
			ShippingMethodID: shippingMethodID,
			Amount:           result.Amount,
			Currency:         result.Currency,
			Email:            req.Email,
			ShippingAddress:  req.ShippingAddress,
			BillingAddress:   billingAddress,
			Status:           "pending",
			ExpiresAt:        time.Now().Add(paymentLinkExpiry),
		}

		if err := insertPaymentLink(r.Context(), db, link); err != nil {
			logger.Error().Err(err).Msg("stripe: insert payment link")
			writeError(w, http.StatusInternalServerError, "failed to save payment link")
			return
		}

		paymentURL := "/plugins/stripe/pay/" + token
		writeJSON(w, http.StatusCreated, map[string]interface{}{
			"data": map[string]interface{}{
				"payment_url":       paymentURL,
				"payment_intent_id": result.ID,
				"expires_at":        link.ExpiresAt.UTC().Format(time.RFC3339),
			},
		})
	}
}

// paymentLinkGetHandler returns the public data for a payment link by token.
// GET /api/v1/store/stripe/payment-link/{token}
// No auth required — the token itself is the capability.
func paymentLinkGetHandler(db *pgxpool.Pool, logger zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := chi.URLParam(r, "token")
		if token == "" {
			writeError(w, http.StatusBadRequest, "missing token")
			return
		}

		link, err := getPaymentLinkByToken(r.Context(), db, token)
		if err != nil {
			logger.Error().Err(err).Str("token", token).Msg("stripe: get payment link by token")
			writeError(w, http.StatusNotFound, "payment link not found")
			return
		}

		if link.Status != "pending" {
			writeError(w, http.StatusGone, "payment link is no longer active")
			return
		}
		if time.Now().After(link.ExpiresAt) {
			writeError(w, http.StatusGone, "payment link has expired")
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"data": map[string]interface{}{
				"client_secret":   link.ClientSecret,
				"publishable_key": link.PublishableKey,
				"amount":          link.Amount,
				"currency":        link.Currency,
				"email":           link.Email,
			},
		})
	}
}

// paymentLinkCompleteRequest is the body expected by POST /api/v1/store/stripe/payment-link/{token}/complete.
type paymentLinkCompleteRequest struct {
	PaymentIntentID string `json:"payment_intent_id"`
}

// paymentLinkCompleteHandler validates payment, triggers a full Stoa checkout,
// and marks the payment link as completed.
// POST /api/v1/store/stripe/payment-link/{token}/complete
// No auth required — the token itself grants access.
func paymentLinkCompleteHandler(sc *stripeClient, db *pgxpool.Pool, checkoutFn sdk.CheckoutFn, secureCookie bool, logger zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := chi.URLParam(r, "token")
		if token == "" {
			writeError(w, http.StatusBadRequest, "missing token")
			return
		}

		var req paymentLinkCompleteRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, maxPaymentIntentBodyBytes)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if req.PaymentIntentID == "" {
			writeError(w, http.StatusBadRequest, "payment_intent_id is required")
			return
		}

		link, err := getPaymentLinkByToken(r.Context(), db, token)
		if err != nil {
			logger.Error().Err(err).Str("token", token).Msg("stripe: complete payment link — not found")
			writeError(w, http.StatusNotFound, "payment link not found")
			return
		}

		if link.PaymentIntentID != req.PaymentIntentID {
			writeError(w, http.StatusUnprocessableEntity, "payment_intent_id does not match")
			return
		}

		pi, err := sc.RetrievePaymentIntent(r.Context(), req.PaymentIntentID)
		if err != nil {
			logger.Error().Err(err).Str("payment_intent_id", req.PaymentIntentID).Msg("stripe: retrieve payment intent for completion")
			writeError(w, http.StatusBadGateway, "failed to verify payment intent")
			return
		}

		piStatus := string(pi.Status)
		if piStatus != "requires_capture" && piStatus != "succeeded" {
			writeError(w, http.StatusUnprocessableEntity, "payment intent is not in a capturable state")
			return
		}

		// Atomically claim the link BEFORE checkout to prevent race conditions.
		// Only one concurrent request can succeed here.
		completed, err := completePaymentLink(r.Context(), db, token)
		if err != nil {
			logger.Error().Err(err).Str("token", token).Msg("stripe: complete payment link — db update")
			writeError(w, http.StatusInternalServerError, "failed to process payment link")
			return
		}
		if !completed {
			writeError(w, http.StatusConflict, "payment link was already completed")
			return
		}

		// Load cart items and build checkout request.
		cartItems, err := loadCartItems(r.Context(), db, link.CartID)
		if err != nil {
			logger.Error().Err(err).Str("cart_id", link.CartID.String()).Msg("stripe: load cart items for payment link checkout")
			revertPaymentLink(r.Context(), db, token)
			writeError(w, http.StatusInternalServerError, "failed to load cart")
			return
		}
		if len(cartItems) == 0 {
			revertPaymentLink(r.Context(), db, token)
			writeError(w, http.StatusUnprocessableEntity, "cart is empty")
			return
		}

		checkoutReq := buildCheckoutRequest(link, cartItems)
		reqJSON, err := json.Marshal(checkoutReq)
		if err != nil {
			logger.Error().Err(err).Msg("stripe: marshal checkout request")
			revertPaymentLink(r.Context(), db, token)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		// Trigger full Stoa checkout (validation, prices, tax, stock, hooks/capture).
		orderJSON, err := checkoutFn(r.Context(), link.CustomerID, reqJSON)
		if err != nil {
			logger.Error().Err(err).Str("token", token).Msg("stripe: payment link checkout failed")
			revertPaymentLink(r.Context(), db, token)
			writeError(w, http.StatusInternalServerError, "checkout failed")
			return
		}

		// Extract order info from checkout response.
		var orderResp struct {
			Data struct {
				ID         string `json:"id"`
				GuestToken string `json:"guest_token,omitempty"`
				Total      int64  `json:"total"`
			} `json:"data"`
		}
		if err := json.Unmarshal(orderJSON, &orderResp); err != nil {
			logger.Error().Err(err).Msg("stripe: unmarshal checkout response")
			// Still return success — the order was created.
		}

		// Warn if the order total diverges from the PaymentIntent amount.
		// This can happen if the cart was modified between link creation and completion.
		if orderResp.Data.Total > 0 && orderResp.Data.Total != link.Amount {
			logger.Warn().
				Int64("order_total", orderResp.Data.Total).
				Int64("pi_amount", link.Amount).
				Str("order_id", orderResp.Data.ID).
				Str("payment_intent_id", req.PaymentIntentID).
				Msg("stripe: payment link amount mismatch — manual review recommended")
		}

		// Set guest token cookie for guest checkouts.
		if link.CustomerID == nil && orderResp.Data.GuestToken != "" {
			http.SetCookie(w, &http.Cookie{
				Name:     "stoa_guest_token",
				Value:    orderResp.Data.GuestToken,
				Path:     "/api/v1/store",
				HttpOnly: true,
				Secure:   secureCookie,
				SameSite: http.SameSiteLaxMode,
				MaxAge:   86400 * 30,
			})
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"data": map[string]interface{}{
				"status":            "completed",
				"payment_intent_id": req.PaymentIntentID,
				"order_id":          orderResp.Data.ID,
			},
		})
	}
}

// calculateCartTotal computes the server-side order total from cart items + shipping.
// It queries product/variant prices from the DB to prevent client-side manipulation.
func calculateCartTotal(ctx context.Context, db *pgxpool.Pool, cartID, shippingMethodID uuid.UUID) (int64, error) {
	if db == nil {
		return 0, fmt.Errorf("database connection required")
	}
	items, err := loadCartItems(ctx, db, cartID)
	if err != nil {
		return 0, err
	}
	if len(items) == 0 {
		return 0, fmt.Errorf("cart is empty")
	}

	var total int64
	for _, item := range items {
		var priceGross int
		if item.VariantID != nil {
			err = db.QueryRow(ctx,
				`SELECT COALESCE(pv.price_gross, p.price_gross)
				 FROM products p
				 JOIN product_variants pv ON pv.product_id = p.id
				 WHERE p.id = $1 AND pv.id = $2 AND p.active = true AND pv.active = true`,
				item.ProductID, *item.VariantID).Scan(&priceGross)
		} else {
			err = db.QueryRow(ctx,
				`SELECT price_gross FROM products WHERE id = $1 AND active = true`,
				item.ProductID).Scan(&priceGross)
		}
		if err != nil {
			return 0, fmt.Errorf("resolving price for product %s: %w", item.ProductID, err)
		}
		total += int64(priceGross) * int64(item.Quantity)
	}

	var shippingGross int
	err = db.QueryRow(ctx,
		`SELECT price_gross FROM shipping_methods WHERE id = $1 AND active = true`,
		shippingMethodID).Scan(&shippingGross)
	if err != nil {
		return 0, fmt.Errorf("resolving shipping price: %w", err)
	}
	total += int64(shippingGross)

	return total, nil
}

// cartItemRow represents a single item from the cart_items table.
type cartItemRow struct {
	ProductID uuid.UUID
	VariantID *uuid.UUID
	Quantity  int
}

// loadCartItems fetches all items for a given cart.
func loadCartItems(ctx context.Context, db *pgxpool.Pool, cartID uuid.UUID) ([]cartItemRow, error) {
	rows, err := db.Query(ctx,
		`SELECT product_id, variant_id, quantity FROM cart_items WHERE cart_id = $1`, cartID)
	if err != nil {
		return nil, fmt.Errorf("querying cart items: %w", err)
	}
	defer rows.Close()

	var items []cartItemRow
	for rows.Next() {
		var item cartItemRow
		if err := rows.Scan(&item.ProductID, &item.VariantID, &item.Quantity); err != nil {
			return nil, fmt.Errorf("scanning cart item: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// buildCheckoutRequest constructs a checkout request from a payment link and cart items.
func buildCheckoutRequest(link *PaymentLink, items []cartItemRow) map[string]interface{} {
	var shippingAddr, billingAddr map[string]interface{}
	json.Unmarshal(link.ShippingAddress, &shippingAddr) //nolint:errcheck
	if len(link.BillingAddress) > 0 {
		json.Unmarshal(link.BillingAddress, &billingAddr) //nolint:errcheck
	} else {
		billingAddr = shippingAddr
	}

	checkoutItems := make([]map[string]interface{}, len(items))
	for i, item := range items {
		ci := map[string]interface{}{
			"product_id": item.ProductID.String(),
			"quantity":   item.Quantity,
		}
		if item.VariantID != nil {
			ci["variant_id"] = item.VariantID.String()
		}
		checkoutItems[i] = ci
	}

	return map[string]interface{}{
		"currency":           link.Currency,
		"billing_address":    billingAddr,
		"shipping_address":   shippingAddr,
		"payment_method_id":  link.PaymentMethodID.String(),
		"shipping_method_id": link.ShippingMethodID.String(),
		"payment_reference":  link.PaymentIntentID,
		"items":              checkoutItems,
	}
}

// paymentStatusHandler returns the current status of a payment by its Stripe PaymentIntent ID.
// GET /api/v1/store/stripe/payment-status/{paymentIntentID}
// OptionalAuth — verifies ownership via PI metadata before returning status.
func paymentStatusHandler(sc *stripeClient, db *pgxpool.Pool, authHelper *sdk.AuthHelper, logger zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		piID := chi.URLParam(r, "paymentIntentID")
		if piID == "" {
			writeError(w, http.StatusBadRequest, "missing paymentIntentID")
			return
		}

		pi, err := sc.RetrievePaymentIntent(r.Context(), piID)
		if err != nil {
			logger.Error().Err(err).Str("payment_intent_id", piID).Msg("stripe: retrieve payment intent for status")
			writeError(w, http.StatusBadGateway, "failed to retrieve payment intent")
			return
		}

		// Ownership check: verify the requesting user owns this PaymentIntent.
		customerID := authHelper.UserID(r.Context())
		if customerID != uuid.Nil {
			// Authenticated user: must match stoa_customer_id in PI metadata.
			if pi.Metadata["stoa_customer_id"] != customerID.String() {
				writeError(w, http.StatusNotFound, "payment intent not found")
				return
			}
		} else {
			// Guest: must provide matching guest token via header or cookie.
			guestToken := r.Header.Get("X-Guest-Token")
			if guestToken == "" {
				if c, err := r.Cookie("stoa_guest_token"); err == nil {
					guestToken = c.Value
				}
			}
			stoaGuestToken := pi.Metadata["stoa_guest_token"]
			if guestToken == "" || stoaGuestToken == "" || guestToken != stoaGuestToken {
				writeError(w, http.StatusNotFound, "payment intent not found")
				return
			}
		}

		paymentLinkStatus := ""
		if db != nil {
			link, err := getPaymentLinkByPaymentIntentID(r.Context(), db, piID)
			if err != nil {
				logger.Debug().Err(err).Str("payment_intent_id", piID).Msg("stripe: no payment link found for payment intent")
			} else if link != nil {
				paymentLinkStatus = link.Status
			}
		}

		resp := map[string]interface{}{
			"status": string(pi.Status),
		}
		if paymentLinkStatus != "" {
			resp["payment_link_status"] = paymentLinkStatus
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{"data": resp})
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

		// Use a detached context with timeout for async processing.
		// r.Context() is canceled when the handler returns, which would
		// cause DB operations in the goroutine to fail.
		bgCtx, cancel := context.WithTimeout(context.Background(), webhookProcessingTimeout)

		switch event.Type {
		case "payment_intent.succeeded":
			pi, err := unmarshalPaymentIntent(event.Data.Raw)
			if err != nil {
				cancel()
				logger.Error().Err(err).Msg("stripe webhook: unmarshal payment intent")
				writeError(w, http.StatusBadRequest, "invalid event data")
				return
			}
			go func() {
				defer cancel()
				handlePaymentIntentSucceeded(bgCtx, pi, db, hooks, logger)
			}()

		case "payment_intent.payment_failed":
			pi, err := unmarshalPaymentIntent(event.Data.Raw)
			if err != nil {
				cancel()
				logger.Error().Err(err).Msg("stripe webhook: unmarshal payment intent")
				writeError(w, http.StatusBadRequest, "invalid event data")
				return
			}
			go func() {
				defer cancel()
				handlePaymentIntentFailed(bgCtx, pi, db, hooks, logger)
			}()

		default:
			cancel()
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

// insertPendingTransaction creates a "pending" payment_transaction so the
// Admin Panel can display the transaction immediately after PI creation.
// Uses ON CONFLICT DO NOTHING because the webhook may have already arrived
// (race condition in test mode with instant confirmation).
func insertPendingTransaction(
	ctx context.Context,
	db *pgxpool.Pool,
	orderID, paymentMethodID uuid.UUID,
	providerRef string,
	amount int64,
	currency string,
) error {
	_, err := db.Exec(ctx, `
		INSERT INTO payment_transactions
			(id, order_id, payment_method_id, status, amount, currency, provider_reference, created_at)
		VALUES ($1, $2, $3, 'pending', $4, $5, $6, NOW())
		ON CONFLICT (provider_reference) WHERE provider_reference IS NOT NULL DO NOTHING`,
		uuid.New(), orderID, paymentMethodID, amount, strings.ToUpper(currency), providerRef,
	)
	if err != nil {
		return fmt.Errorf("insert pending transaction: %w", err)
	}
	return nil
}

// insertCancelledTransaction creates a "cancelled" payment_transaction so the
// Admin Panel can display the Stripe transaction link for verification even when
// the checkout failed (e.g. insufficient stock).
func insertCancelledTransaction(
	ctx context.Context,
	db *pgxpool.Pool,
	orderID, paymentMethodID uuid.UUID,
	providerRef string,
	amount int64,
	currency string,
) error {
	_, err := db.Exec(ctx, `
		INSERT INTO payment_transactions
			(id, order_id, payment_method_id, status, amount, currency, provider_reference, created_at)
		VALUES ($1, $2, $3, 'cancelled', $4, $5, $6, NOW())
		ON CONFLICT (provider_reference) WHERE provider_reference IS NOT NULL DO NOTHING`,
		uuid.New(), orderID, paymentMethodID, amount, strings.ToUpper(currency), providerRef,
	)
	if err != nil {
		return fmt.Errorf("insert cancelled transaction: %w", err)
	}
	return nil
}

// extractReceiptEmail extracts the email from a JSON-encoded billing address.
// Returns an empty string if the address is nil, empty, or contains no email.
func extractReceiptEmail(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var addr struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(raw, &addr); err != nil {
		return ""
	}
	return addr.Email
}
