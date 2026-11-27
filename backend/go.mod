module github.com/savisaluwadana/KiroFrankenstein/backend

go 1.23.0

require (
	github.com/go-chi/chi/v5 v5.2.2 // HTTP router for internal/httpapi
	github.com/jackc/pgx/v5 v5.7.5 // PostgreSQL driver for internal/store/postgres
	github.com/pressly/goose/v3 v3.24.3 // SQL migrations (backend/migrations)
	pgregory.net/rapid v1.3.0 // property-based tests (TestProp*)
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/sethvargo/go-retry v0.3.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/crypto v0.38.0 // indirect
	golang.org/x/sync v0.14.0 // indirect
	golang.org/x/text v0.25.0 // indirect
)
