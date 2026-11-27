package postgres_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const envMigrateURL = "PARALLEL_TEST_MIGRATE_DATABASE_URL"

const (
	codeRestrictViolation = "23001" // raised by the input_log_immutable trigger
	codeInsufficientPriv  = "42501" // permission denied
)

// TestInputLogImmutable proves INV-3 at the database layer (Req 28.5, 13.2).
//
// Layer 1 connects as the table owner (parallel_migrator), whose privileges
// would otherwise allow UPDATE, DELETE and TRUNCATE, so any rejection must come
// from the triggers. Layer 2 connects as the runtime role (parallel_app), which
// must be refused by privileges before any trigger runs.
//
// Fixture rows can never be deleted, so each run inserts rows under fresh
// random UUIDs; the test database accrues a few rows per run. That is expected.
func TestInputLogImmutable(t *testing.T) {
	appPool := testPool(t)
	migURL := os.Getenv(envMigrateURL)
	if migURL == "" {
		t.Skipf("%s not set; skipping input_log immutability test (see README, Testing)", envMigrateURL)
	}
	ctx := context.Background()

	owner, err := pgx.Connect(ctx, migURL)
	if err != nil {
		t.Fatalf("connect as migrator: %v", err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })

	var current string
	if err := owner.QueryRow(ctx, `SELECT current_user`).Scan(&current); err != nil || current != "parallel_migrator" {
		t.Fatalf("migrate URL must connect as parallel_migrator, got %q (err %v)", current, err)
	}

	// Fixtures through the runtime role (INSERT is the only write it has).
	ids := newIDMap()
	worldID, branchID := ids.to("world"), ids.to("branch")
	if _, err := appPool.Exec(ctx,
		`INSERT INTO worlds (id, name, scenario_hash, scenario_json, seed, engine_version)
		 VALUES ($1, 'immutability', $2, '{}'::bytea, 1, '1')`, worldID, make([]byte, 32)); err != nil {
		t.Fatalf("insert world: %v", err)
	}
	if _, err := appPool.Exec(ctx,
		`INSERT INTO branches (id, world_id, label, manager_kind) VALUES ($1, $2, 'root', 'human')`,
		branchID, worldID); err != nil {
		t.Fatalf("insert branch: %v", err)
	}
	var rowID int64
	if err := appPool.QueryRow(ctx,
		`INSERT INTO input_log (branch_id, seq, effective_tick, kind, payload, provenance)
		 VALUES ($1, 1, 5, 0, $2, 'human') RETURNING id`, branchID, []byte("original")).Scan(&rowID); err != nil {
		t.Fatalf("insert input: %v", err)
	}

	type snap struct {
		payload []byte
		tick    int
		count   int
	}
	read := func(q interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	}) snap {
		var s snap
		if err := q.QueryRow(ctx, `SELECT payload, effective_tick FROM input_log WHERE id = $1`, rowID).Scan(&s.payload, &s.tick); err != nil {
			t.Fatalf("select row: %v", err)
		}
		if err := q.QueryRow(ctx, `SELECT count(*) FROM input_log WHERE branch_id = $1`, branchID).Scan(&s.count); err != nil {
			t.Fatalf("count rows: %v", err)
		}
		return s
	}
	before := read(owner)

	pgErr := func(t *testing.T, err error) *pgconn.PgError {
		t.Helper()
		var pe *pgconn.PgError
		if !errors.As(err, &pe) {
			t.Fatalf("want a PostgreSQL error, got %v", err)
		}
		return pe
	}

	stmts := []struct{ name, sql string }{
		{"update", `UPDATE input_log SET payload = 'tampered', effective_tick = 99 WHERE id = $1`},
		{"delete", `DELETE FROM input_log WHERE id = $1`},
		{"truncate", `TRUNCATE input_log`},
	}

	// Layer 1: the owner is stopped by the triggers, not by permissions.
	for _, s := range stmts {
		t.Run("owner_"+s.name, func(t *testing.T) {
			// Run in a transaction that is always rolled back, so a missing
			// trigger could not destroy data even in a shared database.
			tx, err := owner.Begin(ctx)
			if err != nil {
				t.Fatalf("begin: %v", err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			var execErr error
			if s.name == "truncate" {
				_, execErr = tx.Exec(ctx, s.sql)
			} else {
				_, execErr = tx.Exec(ctx, s.sql, rowID)
			}
			if execErr == nil {
				t.Fatalf("owner %s on input_log succeeded; the trigger did not fire", s.name)
			}
			pe := pgErr(t, execErr)
			if pe.Code != codeRestrictViolation || !strings.Contains(pe.Message, "input_log is append-only") {
				t.Fatalf("owner %s: want trigger error %s 'input_log is append-only', got %s %q",
					s.name, codeRestrictViolation, pe.Code, pe.Message)
			}
			t.Logf("owner %s rejected by trigger: [%s] %s", s.name, pe.Code, pe.Message)
		})
	}

	// Layer 2: the runtime role has no privilege for any of the three.
	for _, s := range stmts {
		t.Run("runtime_"+s.name, func(t *testing.T) {
			var execErr error
			if s.name == "truncate" {
				_, execErr = appPool.Exec(ctx, s.sql)
			} else {
				_, execErr = appPool.Exec(ctx, s.sql, rowID)
			}
			if execErr == nil {
				t.Fatalf("runtime %s on input_log succeeded", s.name)
			}
			pe := pgErr(t, execErr)
			if pe.Code != codeInsufficientPriv {
				t.Fatalf("runtime %s: want permission denied %s, got %s %q", s.name, codeInsufficientPriv, pe.Code, pe.Message)
			}
			t.Logf("runtime %s refused: [%s] %s", s.name, pe.Code, pe.Message)
		})
	}

	after := read(owner)
	if !bytes.Equal(before.payload, after.payload) || before.tick != after.tick || before.count != after.count {
		t.Fatalf("input_log row changed: before %+v, after %+v", before, after)
	}
	if string(after.payload) != "original" || after.tick != 5 || after.count != 1 {
		t.Fatalf("input_log row not as inserted: %+v", after)
	}
}
