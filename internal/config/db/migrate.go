package db

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/IvanSaratov/go-metrics-practice/migrations"
	"github.com/pressly/goose/v3"
)

func Migrate(ctx context.Context, database *sql.DB) error {
	provider, err := goose.NewProvider(goose.DialectPostgres, database, migrations.Files)
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}

	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("apply database migrations: %w", err)
	}

	return nil
}
