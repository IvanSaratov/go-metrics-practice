package db

import (
	"database/sql"
	"fmt"

	"github.com/lib/pq"
)

// Создание пула соединений
func Open(dsn string) (*sql.DB, error) {
	connector, err := pq.NewConnector(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse PostgreSQL DSN: %w", err)
	}

	return sql.OpenDB(connector), nil
}
