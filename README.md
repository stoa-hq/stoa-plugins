# stoa-plugins

Official plugin monorepo for [Stoa](https://github.com/stoa-hq/stoa) — headless e-commerce for humans and agents.

Each plugin is an independent Go module. Install only what you need.

## Available plugins

| Plugin | Description | Version |
|--------|-------------|---------|
| [`n8n`](./n8n) | Workflow automation & cronjobs via n8n | `v0.1.2` |
| [`stripe`](./stripe)| stripe payment method | `v0.1.1` |

## Installation

```bash
go get github.com/stoa-hq/stoa-plugins/n8n@latest
```

Register the plugin in your Stoa app (`internal/app/app.go`):

```go
import n8nplugin "github.com/stoa-hq/stoa-plugins/n8n"

func (a *App) RegisterPlugins() error {
    return a.PluginRegistry.Register(n8nplugin.New(), appCtx)
}
```

Full configuration and usage docs: [stoa-hq.github.io/docs/plugins](https://github.com/stoa-hq/stoa)

## Structure

```
stoa-plugins/
├── n8n/             # Workflow automation via n8n
│   ├── go.mod
│   ├── plugin.go
│   └── ...
└── shared/          # Shared utilities (planned)
```

Each plugin is an independent Go module with its own `go.mod`, versioned via subdirectory tags (e.g. `n8n/v1.0.0`).

## Local development

```bash
go work init
go work use ./n8n
```

## Contributing

1. Branch from `main`: `git checkout -b feat/plugin-name`
2. Create a subdirectory with its own `go.mod` (`module github.com/stoa-hq/stoa-plugins/<name>`)
3. Implement `sdk.Plugin` from `github.com/stoa-hq/stoa/pkg/sdk`
4. Add tests — every implementation requires test coverage
5. Document the plugin in [stoa-docs](https://github.com/stoa-hq/docs) under `docs/plugins/`

## License

[Apache 2.0](./LICENSE)
