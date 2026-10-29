// Package db создаёт пул соединений с PostgreSQL и применяет миграции.
package db

import (
	"database/sql"
	"fmt"

	"github.com/lib/pq"
)

// Open создаёт пул соединений с PostgreSQL по строке dsn.
// Подключение к базе не проверяет; для этого нужен PingContext.
func Open(dsn string) (*sql.DB, error) {
	connector, err := pq.NewConnector(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse PostgreSQL DSN: %w", err)
	}

	return sql.OpenDB(connector), nil
}
