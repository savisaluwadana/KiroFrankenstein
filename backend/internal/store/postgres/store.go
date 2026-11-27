package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/app"
	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// Store is the PostgreSQL implementation of app.Store. It uses only
// parameterized queries and needs only the grants of the runtime role
// (no DDL; no UPDATE or DELETE on input_log).
type Store struct {
	pool *pgxpool.Pool
}

var _ app.Store = (*Store)(nil)

// New wraps an existing pool. The caller owns the pool.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Connect opens a pool for url and verifies connectivity.
func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("postgres: open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}
	return pool, nil
}

// ErrNotMigrated means the schema is missing; run `parallel-api migrate up`.
var ErrNotMigrated = errors.New("postgres: schema not migrated")

// CheckMigrated verifies that every table the store needs exists. It uses
// to_regclass, which needs no privilege on the tables themselves.
func CheckMigrated(ctx context.Context, pool *pgxpool.Pool) error {
	for _, t := range []string{"worlds", "branches", "input_log", "snapshots", "tick_summaries"} {
		var reg *string
		if err := pool.QueryRow(ctx, `SELECT to_regclass($1)::text`, "public."+t).Scan(&reg); err != nil {
			return fmt.Errorf("postgres: check table %s: %w", t, err)
		}
		if reg == nil {
			return fmt.Errorf("table %s is missing: %w", t, ErrNotMigrated)
		}
	}
	return nil
}

// PostgreSQL SQLSTATE codes the store maps to app errors.
const (
	codeUniqueViolation = "23505"
	codeForeignKey      = "23503"
	codeInvalidTextRepr = "22P02" // malformed uuid
)

func pgCode(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

// seedToDB and seedFromDB map uint64 to bigint by two's complement bit pattern.
func seedToDB(s sim.Seed) int64   { return int64(uint64(s)) }
func seedFromDB(v int64) sim.Seed { return sim.Seed(uint64(v)) }

// tickToDB converts a tick to the integer column type, rejecting values that do
// not fit instead of truncating.
func tickToDB(t sim.Tick) (int32, error) {
	if t < 0 || t > math.MaxInt32 {
		return 0, fmt.Errorf("postgres: tick %d out of range", t)
	}
	return int32(t), nil
}

// tickLimitToDB clamps an upper bound: any tick above MaxInt32 is "everything".
func tickLimitToDB(t sim.Tick) int32 {
	if t > math.MaxInt32 {
		return math.MaxInt32
	}
	if t < -1 {
		return -1
	}
	return int32(t)
}
