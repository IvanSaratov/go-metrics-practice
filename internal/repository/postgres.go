package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

const (
	setGaugeQuery = `
		INSERT INTO gauges (name, value)
		VALUES ($1, $2)
		ON CONFLICT (name) DO UPDATE
		SET value = EXCLUDED.value`
	addCounterQuery = `
		INSERT INTO counters (name, value)
		VALUES ($1, $2)
		ON CONFLICT (name) DO UPDATE
		SET value = counters.value + EXCLUDED.value
		RETURNING value`
	getGaugeQuery       = `SELECT value FROM gauges WHERE name = $1`
	getCounterQuery     = `SELECT value FROM counters WHERE name = $1`
	getAllGaugesQuery   = `SELECT name, value FROM gauges`
	getAllCountersQuery = `SELECT name, value FROM counters`
)

type PostgresStorage struct {
	database *sql.DB
}

func NewPostgresStorage(database *sql.DB) *PostgresStorage {
	return &PostgresStorage{database: database}
}

func (s *PostgresStorage) SetGauge(ctx context.Context, name string, value float64) error {
	if _, err := s.database.ExecContext(ctx, setGaugeQuery, name, value); err != nil {
		return fmt.Errorf("set gauge %q: %w", name, err)
	}

	return nil
}

func (s *PostgresStorage) AddCounter(ctx context.Context, name string, value int64) (int64, error) {
	var total int64
	if err := s.database.QueryRowContext(ctx, addCounterQuery, name, value).Scan(&total); err != nil {
		return 0, fmt.Errorf("add counter %q: %w", name, err)
	}

	return total, nil
}

func (s *PostgresStorage) GetGauge(ctx context.Context, name string) (float64, bool, error) {
	var value float64
	err := s.database.QueryRowContext(ctx, getGaugeQuery, name).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("get gauge %q: %w", name, err)
	}

	return value, true, nil
}

func (s *PostgresStorage) GetCounter(ctx context.Context, name string) (int64, bool, error) {
	var value int64
	err := s.database.QueryRowContext(ctx, getCounterQuery, name).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("get counter %q: %w", name, err)
	}

	return value, true, nil
}

func (s *PostgresStorage) GetAllGauges(ctx context.Context) (map[string]float64, error) {
	rows, err := s.database.QueryContext(ctx, getAllGaugesQuery)
	if err != nil {
		return nil, fmt.Errorf("get all gauges: %w", err)
	}
	defer rows.Close()

	values := make(map[string]float64)
	for rows.Next() {
		var name string
		var value float64
		if err := rows.Scan(&name, &value); err != nil {
			return nil, fmt.Errorf("scan gauge: %w", err)
		}
		values[name] = value
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate gauges: %w", err)
	}

	return values, nil
}

func (s *PostgresStorage) GetAllCounters(ctx context.Context) (map[string]int64, error) {
	rows, err := s.database.QueryContext(ctx, getAllCountersQuery)
	if err != nil {
		return nil, fmt.Errorf("get all counters: %w", err)
	}
	defer rows.Close()

	values := make(map[string]int64)
	for rows.Next() {
		var name string
		var value int64
		if err := rows.Scan(&name, &value); err != nil {
			return nil, fmt.Errorf("scan counter: %w", err)
		}
		values[name] = value
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate counters: %w", err)
	}

	return values, nil
}
