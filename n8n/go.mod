module github.com/stoa-hq/stoa-plugins/n8n

go 1.25.5

require (
	github.com/rs/zerolog v1.34.0
	github.com/stoa-hq/stoa v0.0.0
)

require github.com/go-chi/chi/v5 v5.2.5

require (
	github.com/google/uuid v1.6.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/pgx/v5 v5.8.0 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.19 // indirect
	golang.org/x/sync v0.19.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	golang.org/x/text v0.34.0 // indirect
)

replace github.com/stoa-hq/stoa => ../../stoa
