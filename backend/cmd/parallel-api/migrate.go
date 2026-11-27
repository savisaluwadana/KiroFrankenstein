package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/pressly/goose/v3"

	"github.com/savisaluwadana/KiroFrankenstein/backend/migrations"
)

// runMigrate implements `parallel-api migrate up|down|status`. It connects with
// MIGRATE_DATABASE_URL (the owner role), never DATABASE_URL (the runtime role,
// which has no DDL). Connection strings are never logged or echoed.
func runMigrate(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: parallel-api migrate up|down|status")
	}
	dsn := os.Getenv("MIGRATE_DATABASE_URL")
	if dsn == "" {
		return errors.New("MIGRATE_DATABASE_URL is not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return errors.New("cannot open migration database (check MIGRATE_DATABASE_URL syntax)")
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return errors.New("cannot connect to the migration database (check MIGRATE_DATABASE_URL, role and that the database is running)")
	}
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	switch args[0] {
	case "up":
		return goose.UpContext(ctx, db, ".")
	case "down":
		return goose.DownContext(ctx, db, ".")
	case "status":
		return goose.StatusContext(ctx, db, ".")
	default:
		return fmt.Errorf("unknown migrate command %q (want up, down or status)", args[0])
	}
}
