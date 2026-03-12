# stoa-plugin-n8n

Forwards Stoa domain events to [n8n](https://n8n.io) via signed HTTP webhooks — enabling business workflows, cronjobs, and integrations without coupling them to the Stoa core.

## How it works

```
Stoa Hook (e.g. order.after_create)
    │
    ▼
stoa-plugin-n8n
    │  POST {webhook_base_url}/order.after_create
    │  X-Stoa-Signature: sha256=<hmac>
    ▼
n8n Webhook Node
    │
    ├── Send order confirmation e-mail
    ├── Notify warehouse via Slack
    ├── Sync to ERP
    └── ...
```

## Registered hooks

All `after_*` hooks are forwarded. Before-hooks are intentionally excluded — they can abort operations and should not be the responsibility of a notification integration.

| Hook | Payload |
|---|---|
| `product.after_create` / `update` / `delete` | Product entity |
| `category.after_create` / `update` / `delete` | Category entity |
| `order.after_create` / `update` | Order entity |
| `cart.after_add_item` / `update_item` / `remove_item` | Cart item |
| `customer.after_create` / `update` | Customer entity |
| `payment.after_complete` / `after_failed` | Payment entity |
| `checkout.after` | Checkout result |

## Configuration

```yaml
# config.yaml
plugins:
  n8n:
    webhook_base_url: "http://n8n:5678/webhook/stoa"
    secret: "change-me-to-a-strong-secret"
    timeout_seconds: 10   # optional, default: 10
```

## Webhook payload

```json
{
  "event": "order.after_create",
  "timestamp": "2026-03-12T10:00:00Z",
  "entity": { "id": "...", "..." : "..." },
  "changes": {},
  "metadata": {}
}
```

## Signature verification in n8n

Every request carries `X-Stoa-Signature: sha256=<hmac-sha256(body, secret)>`.

In your n8n webhook node, add a **Header Auth** credential or use a **Code node** to verify:

```javascript
const crypto = require('crypto');
const body = JSON.stringify($input.item.json);
const sig = $input.item.headers['x-stoa-signature'];
const expected = 'sha256=' + crypto.createHmac('sha256', 'your-secret').update(body).digest('hex');
if (sig !== expected) throw new Error('Invalid signature');
```

## Health endpoint

```
GET /plugins/n8n/health
```

Returns `200 OK` when n8n is reachable, `503 Service Unavailable` otherwise.

```json
{
  "status": "ok",
  "n8n_reachable": true,
  "checked_at": "2026-03-12T10:00:00Z"
}
```

## Registration in Stoa

```go
// cmd/stoa/main.go or wherever plugins are registered
import n8nplugin "github.com/stoa-hq/stoa-plugins/n8n"

pluginRegistry.Register(n8nplugin.New(), appCtx)
```
