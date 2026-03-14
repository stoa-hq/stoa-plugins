// Package stripe provides a Stoa plugin that integrates Stripe as a payment provider.
//
// The plugin exposes two HTTP endpoints:
//   - POST /api/v1/store/stripe/payment-intent — creates a Stripe PaymentIntent for a
//     pending order; the client_secret returned can be used with Stripe.js or the
//     Stripe Mobile SDKs to confirm payment on the frontend.
//   - POST /plugins/stripe/webhook — receives signed Stripe webhook events;
//     on payment_intent.succeeded the order status is moved to "confirmed" and a
//     payment_transaction record is created; on payment_intent.payment_failed the
//     payment.after_failed hook is dispatched.
//
// # Configuration (config.yaml)
//
//	plugins:
//	  stripe:
//	    secret_key:      "sk_test_..."
//	    publishable_key: "pk_test_..."
//	    webhook_secret:  "whsec_..."
//	    currency:        "EUR"   # optional, default EUR
//
// # Stripe webhook setup
//
// In the Stripe Dashboard → Webhooks, add an endpoint pointing to:
//
//	https://your-store.example.com/plugins/stripe/webhook
//
// and subscribe to the following events:
//   - payment_intent.succeeded
//   - payment_intent.payment_failed
package stripe

import (
	"net/http"

	"github.com/rs/zerolog"
	"github.com/stoa-hq/stoa/pkg/sdk"
)

const (
	pluginName    = "stripe"
	pluginVersion = "0.2.0"
)

// Plugin integrates Stripe as a payment provider for Stoa.
type Plugin struct {
	sc     *stripeClient
	logger zerolog.Logger
}

// New returns a new Stripe Plugin ready to be registered.
func New() *Plugin { return &Plugin{} }

func init() { sdk.Register(New()) }

func (p *Plugin) Name() string        { return pluginName }
func (p *Plugin) Version() string     { return pluginVersion }
func (p *Plugin) Description() string { return "Stripe payment provider for Stoa" }

// Init reads config, creates the Stripe client, mounts HTTP routes, and
// serves embedded frontend assets via the AssetRouter.
func (p *Plugin) Init(app *sdk.AppContext) error {
	p.logger = app.Logger.With().Str("plugin", pluginName).Logger()

	cfg, err := configFrom(app.Config)
	if err != nil {
		return err
	}

	p.sc = newStripeClient(cfg)

	mountRoutes(app.Router, p.sc, app.DB, app.Hooks, app.Auth, cfg.WebhookSecret, p.logger)

	// Serve embedded frontend assets at /plugins/stripe/assets/*
	if app.AssetRouter != nil {
		app.AssetRouter.Handle("/*", http.FileServer(assetsSubFS()))
	}

	p.logger.Info().
		Str("currency", cfg.Currency).
		Msg("stripe plugin initialised")

	return nil
}

// UIExtensions returns the UI extensions provided by the Stripe plugin.
// It declares a Web Component for the storefront checkout payment slot.
func (p *Plugin) UIExtensions() []sdk.UIExtension {
	return []sdk.UIExtension{
		{
			ID:   "stripe_checkout",
			Slot: "storefront:checkout:payment",
			Type: "component",
			Component: &sdk.UIComponent{
				TagName:         "stoa-stripe-checkout",
				ScriptURL:       "/plugins/stripe/assets/checkout.js",
				Integrity:       sriHash("frontend/dist/checkout.js"),
				ExternalScripts: []string{
					"https://js.stripe.com/v3/",
					"https://api.stripe.com",
				},
			},
		},
	}
}

// Shutdown is a no-op; the Stripe client has no persistent connections.
func (p *Plugin) Shutdown() error { return nil }
