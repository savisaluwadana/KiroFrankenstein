// Package deps keeps the pinned, planned dependencies in go.mod until the
// packages that use them are written (go mod tidy would otherwise drop them).
// Each is justified below. Delete an import here once real code imports it.
//
//   - chi/v5: HTTP router for internal/httpapi.
//   - pgx/v5: PostgreSQL driver for internal/store/postgres.
//   - goose/v3: SQL migrations under backend/migrations.
//   - rapid: property-based tests (TestProp*).
//
// Nothing in internal/sim imports this package.
package deps

import (
	_ "github.com/go-chi/chi/v5"
	_ "github.com/jackc/pgx/v5"
	_ "github.com/pressly/goose/v3"
	_ "pgregory.net/rapid"
)
