package main

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/store/memory"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/store/postgres"
)

// openStore picks the store: PostgreSQL when DATABASE_URL is set, memory
// otherwise. Errors and the returned description never contain the URL.
func openStore(ctx context.Context) (app.Store, func(), string, error) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return memory.New(), func() {}, "memory (in-process: all worlds are lost when the server stops)", nil
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	pool, err := postgres.Connect(cctx, url)
	if err != nil {
		return nil, nil, "", errors.New("cannot connect to PostgreSQL: check DATABASE_URL syntax, credentials and that the database is running")
	}
	if err := postgres.CheckMigrated(cctx, pool); err != nil {
		pool.Close()
		if errors.Is(err, postgres.ErrNotMigrated) {
			return nil, nil, "", errors.New("the database schema is not migrated: run `parallel-api migrate up` with MIGRATE_DATABASE_URL first")
		}
		return nil, nil, "", errors.New("cannot verify the database schema (is DATABASE_URL the runtime role and the database reachable?)")
	}
	return postgres.New(pool), pool.Close, "postgresql", nil
}
