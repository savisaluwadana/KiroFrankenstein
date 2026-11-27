package main

import (
	"context"
	"errors"
	"time"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/store/memory"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/store/postgres"
)

// storeHandle is the opened store and its readiness probes.
type storeHandle struct {
	store       app.Store
	close       func()
	desc        string
	memory      bool
	ping        func(context.Context) error // nil for the memory store
	checkSchema func(context.Context) error
}

// openStore picks the store: PostgreSQL when url is set, memory otherwise.
// Errors and the returned description never contain the URL.
func openStore(ctx context.Context, url string) (storeHandle, error) {
	if url == "" {
		return storeHandle{store: memory.New(), close: func() {}, memory: true,
			desc: "memory (in-process: all worlds are lost when the server stops)"}, nil
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	pool, err := postgres.Connect(cctx, url)
	if err != nil {
		return storeHandle{}, errors.New("cannot connect to PostgreSQL: check DATABASE_URL syntax, credentials and that the database is running")
	}
	if err := postgres.CheckMigrated(cctx, pool); err != nil {
		pool.Close()
		if errors.Is(err, postgres.ErrNotMigrated) {
			return storeHandle{}, errors.New("the database schema is not migrated: run `parallel-api migrate up` with MIGRATE_DATABASE_URL first")
		}
		return storeHandle{}, errors.New("cannot verify the database schema (is DATABASE_URL the runtime role and the database reachable?)")
	}
	return storeHandle{store: postgres.New(pool), close: pool.Close, desc: "postgresql",
		ping:        pool.Ping,
		checkSchema: func(c context.Context) error { return postgres.CheckMigrated(c, pool) },
	}, nil
}
