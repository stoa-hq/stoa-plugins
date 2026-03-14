// Package n8n provides a Stoa plugin that forwards domain events to an n8n
// workflow automation instance via signed HTTP webhooks.
//
// Each Stoa hook (e.g. order.after_create) triggers a POST request to
// {n8n.webhook_base_url}/{hook_name}. n8n can then route the event into any
// workflow — sending e-mails, syncing to an ERP, scheduling follow-up jobs, etc.
//
// # Configuration (config.yaml)
//
//	plugins:
//	  n8n:
//	    webhook_base_url: "http://n8n:5678/webhook/stoa"
//	    secret: "change-me"
//	    timeout_seconds: 10
//
// # Webhook security
//
// Every request carries an X-Stoa-Signature header:
//
//	X-Stoa-Signature: sha256=<hmac-sha256(body, secret)>
//
// Verify this in your n8n webhook node to ensure requests originate from Stoa.
package n8n

import (
	"context"

	"github.com/rs/zerolog"
	"github.com/stoa-hq/stoa/pkg/sdk"
)

const (
	pluginName    = "n8n"
	pluginVersion = "0.2.1"
)

// validAfterHooks is the set of all after-hooks the plugin can forward.
var validAfterHooks = map[string]bool{
	sdk.HookAfterProductCreate:  true,
	sdk.HookAfterProductUpdate:  true,
	sdk.HookAfterProductDelete:  true,
	sdk.HookAfterCategoryCreate: true,
	sdk.HookAfterCategoryUpdate: true,
	sdk.HookAfterCategoryDelete: true,
	sdk.HookAfterOrderCreate:    true,
	sdk.HookAfterOrderUpdate:    true,
	sdk.HookAfterCartAdd:        true,
	sdk.HookAfterCartUpdate:     true,
	sdk.HookAfterCartRemove:     true,
	sdk.HookAfterCustomerCreate: true,
	sdk.HookAfterCustomerUpdate: true,
	sdk.HookAfterPaymentComplete: true,
	sdk.HookAfterPaymentFailed:  true,
	sdk.HookAfterCheckout:       true,
}

// Plugin forwards Stoa domain events to n8n via HTTP webhooks.
type Plugin struct {
	d      *dispatcher
	logger zerolog.Logger
}

// New returns a new n8n Plugin ready to be registered.
func New() *Plugin {
	return &Plugin{}
}

func init() {
	sdk.Register(New())
}

func (p *Plugin) Name() string    { return pluginName }
func (p *Plugin) Version() string { return pluginVersion }
func (p *Plugin) Description() string {
	return "Forwards Stoa domain events to n8n for workflow automation and scheduled jobs"
}

// Init reads config, wires up the dispatcher, registers hook handlers, and
// mounts admin routes under /plugins/n8n.
func (p *Plugin) Init(app *sdk.AppContext) error {
	p.logger = app.Logger.With().Str("plugin", pluginName).Logger()

	cfg, err := configFrom(app.Config)
	if err != nil {
		return err
	}

	p.d = newDispatcher(cfg, p.logger)

	p.registerHooks(app.Hooks, cfg.Hooks)
	mountRoutes(app.Router, app.Auth, p.d, p.logger)

	p.logger.Info().
		Str("webhook_base_url", cfg.WebhookBaseURL).
		Msg("n8n plugin initialised")

	return nil
}

// Shutdown is a no-op; the HTTP client has no persistent connections.
func (p *Plugin) Shutdown() error { return nil }

// registerHooks subscribes to after-hooks so domain events are forwarded to n8n.
// When enabled is nil, all after-hooks are registered. When enabled contains a
// subset, only those hooks are registered. Before-hooks are never registered —
// they can abort operations, which is not the responsibility of a notification
// integration.
func (p *Plugin) registerHooks(hooks *sdk.HookRegistry, enabled []string) {
	// Build the list of hooks to register.
	var selected []string
	if enabled != nil {
		selected = enabled
	} else {
		selected = make([]string, 0, len(validAfterHooks))
		for name := range validAfterHooks {
			selected = append(selected, name)
		}
	}

	for _, name := range selected {
		name := name // capture loop var
		hooks.On(name, func(ctx context.Context, event *sdk.HookEvent) error {
			if err := p.d.Send(ctx, event); err != nil {
				// Log but do not propagate — a failed notification must never
				// roll back a completed business transaction.
				p.logger.Error().Err(err).Str("hook", name).Msg("webhook dispatch failed")
			}
			return nil
		})
	}

	p.logger.Info().Int("count", len(selected)).Msg("registered hooks")
}
