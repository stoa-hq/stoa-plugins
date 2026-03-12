package n8n

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/stoa-hq/stoa/pkg/sdk"
)

// payload is the JSON body sent to n8n for every event.
type payload struct {
	Event     string                 `json:"event"`
	Timestamp time.Time              `json:"timestamp"`
	Entity    interface{}            `json:"entity,omitempty"`
	Changes   map[string]interface{} `json:"changes,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// dispatcher sends HookEvents to n8n via HTTP webhook.
type dispatcher struct {
	cfg    Config
	client *http.Client
	logger zerolog.Logger
}

func newDispatcher(cfg Config, logger zerolog.Logger) *dispatcher {
	return &dispatcher{
		cfg:    cfg,
		client: &http.Client{Timeout: cfg.Timeout},
		logger: logger,
	}
}

// Send serialises the event and POSTs it to {WebhookBaseURL}/{event.Name}.
// The request is signed with HMAC-SHA256 via the X-Stoa-Signature header.
func (d *dispatcher) Send(ctx context.Context, event *sdk.HookEvent) error {
	p := payload{
		Event:     event.Name,
		Timestamp: time.Now().UTC(),
		Entity:    event.Entity,
		Changes:   event.Changes,
		Metadata:  event.Metadata,
	}

	body, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("n8n: marshal payload: %w", err)
	}

	url := strings.TrimRight(d.cfg.WebhookBaseURL, "/") + "/" + event.Name
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("n8n: create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Stoa-Signature", "sha256="+sign(body, d.cfg.Secret))
	req.Header.Set("X-Stoa-Token", d.cfg.Secret)

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("n8n: send webhook for %q: %w", event.Name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("n8n: webhook %q returned HTTP %d", event.Name, resp.StatusCode)
	}

	d.logger.Debug().
		Str("event", event.Name).
		Str("url", url).
		Int("status", resp.StatusCode).
		Msg("n8n webhook dispatched")

	return nil
}

// Ping does a lightweight GET to the n8n base URL to check connectivity.
func (d *dispatcher) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.cfg.WebhookBaseURL, nil)
	if err != nil {
		return err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// sign returns the hex-encoded HMAC-SHA256 of body using secret.
func sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
