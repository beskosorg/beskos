package db

import (
	"database/sql"
	"embed"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver for goose
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate applies pending migrations. It is idempotent: goose tracks which version has
// already been applied. Runs at API startup for simplicity rather than as a separate step.
func Migrate(databaseURL string) error {
	sqldb, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("abrir db p/ migrate: %w", err)
	}
	defer sqldb.Close()

	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.Up(sqldb, "migrations")
}
