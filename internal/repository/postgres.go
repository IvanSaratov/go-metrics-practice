package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	models "github.com/IvanSaratov/go-metrics-practice/internal/model"
	retryhelper "github.com/IvanSaratov/go-metrics-practice/internal/retry"
	"github.com/jackc/pgerrcode"
	"github.com/lib/pq"
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
	addCounterBatchQuery = `
		INSERT INTO counters (name, value)
		VALUES ($1, $2)
		ON CONFLICT (name) DO UPDATE
		SET value = counters.value + EXCLUDED.value`
	getGaugeQuery       = `SELECT value FROM gauges WHERE name = $1`
	getCounterQuery     = `SELECT value FROM counters WHERE name = $1`
	getAllGaugesQuery   = `SELECT name, value FROM gauges`
	getAllCountersQuery = `SELECT name, value FROM counters`
)

type PostgresStorage struct {
	database *sql.DB
	retry    func(context.Context, func(context.Context) error) error
}

func NewPostgresStorage(database *sql.DB) *PostgresStorage {
	return &PostgresStorage{
		database: database,
		retry: func(ctx context.Context, operation func(context.Context) error) error {
			return retryhelper.Do(ctx, operation)
		},
	}
}

func (s *PostgresStorage) SetGauge(ctx context.Context, name string, value float64) error {
	err := s.retry(ctx, func(ctx context.Context) error {
		_, err := s.database.ExecContext(ctx, setGaugeQuery, name, value)
		return retryPostgresError(err)
	})
	if err != nil {
		return fmt.Errorf("set gauge %q: %w", name, err)
	}

	return nil
}

func (s *PostgresStorage) AddCounter(ctx context.Context, name string, value int64) (int64, error) {
	var total int64
	err := s.retry(ctx, func(ctx context.Context) error {
		err := s.database.QueryRowContext(ctx, addCounterQuery, name, value).Scan(&total)
		return retryPostgresError(err)
	})
	if err != nil {
		return 0, fmt.Errorf("add counter %q: %w", name, err)
	}

	return total, nil
}

func (s *PostgresStorage) UpdateBatch(ctx context.Context, metrics []models.Metrics) error {
	// Ссылаемся на общий метод
	if err := models.ValidateUpdates(metrics); err != nil {
		return err
	}
	if len(metrics) == 0 {
		return nil
	}

	return s.retry(ctx, func(ctx context.Context) error {
		return retryPostgresError(s.updateBatch(ctx, metrics))
	})
}

func (s *PostgresStorage) updateBatch(ctx context.Context, metrics []models.Metrics) error {
	// Начинаем нашу транзакцию
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin metrics transaction: %w", err)
	}

	// Если что - возвращаем все обратно
	rollback := func(updateErr error) error {
		if err := transaction.Rollback(); err != nil {
			return errors.Join(updateErr, fmt.Errorf("rollback metrics transaction: %w", err))
		}
		return updateErr
	}

	for _, metric := range metrics {
		switch metric.MType {
		case models.Gauge:
			if _, err := transaction.ExecContext(ctx, setGaugeQuery, metric.ID, *metric.Value); err != nil {
				return rollback(fmt.Errorf("update gauge %q: %w", metric.ID, err))
			}
		case models.Counter:
			if _, err := transaction.ExecContext(
				ctx,
				addCounterBatchQuery,
				metric.ID,
				*metric.Delta,
			); err != nil {
				return rollback(fmt.Errorf("update counter %q: %w", metric.ID, err))
			}
		}
	}

	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit metrics transaction: %w", err)
	}

	return nil
}

func (s *PostgresStorage) GetGauge(ctx context.Context, name string) (float64, bool, error) {
	var value float64
	err := s.retry(ctx, func(ctx context.Context) error {
		err := s.database.QueryRowContext(ctx, getGaugeQuery, name).Scan(&value)
		return retryPostgresError(err)
	})
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
	err := s.retry(ctx, func(ctx context.Context) error {
		err := s.database.QueryRowContext(ctx, getCounterQuery, name).Scan(&value)
		return retryPostgresError(err)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("get counter %q: %w", name, err)
	}

	return value, true, nil
}

func (s *PostgresStorage) GetAllGauges(ctx context.Context) (map[string]float64, error) {
	var values map[string]float64
	err := s.retry(ctx, func(ctx context.Context) error {
		rows, err := s.database.QueryContext(ctx, getAllGaugesQuery)
		if err != nil {
			return retryPostgresError(fmt.Errorf("get all gauges: %w", err))
		}
		defer rows.Close()

		currentValues := make(map[string]float64)
		for rows.Next() {
			var name string
			var value float64
			if err := rows.Scan(&name, &value); err != nil {
				return retryPostgresError(fmt.Errorf("scan gauge: %w", err))
			}
			currentValues[name] = value
		}
		if err := rows.Err(); err != nil {
			return retryPostgresError(fmt.Errorf("iterate gauges: %w", err))
		}

		values = currentValues
		return nil
	})
	if err != nil {
		return nil, err
	}

	return values, nil
}

func retryPostgresError(err error) error {
	var postgresError *pq.Error
	if errors.As(err, &postgresError) && pgerrcode.IsConnectionException(string(postgresError.Code)) {
		return retryhelper.RetryableError(err)
	}

	return err
}

func (s *PostgresStorage) GetAllCounters(ctx context.Context) (map[string]int64, error) {
	var values map[string]int64
	err := s.retry(ctx, func(ctx context.Context) error {
		rows, err := s.database.QueryContext(ctx, getAllCountersQuery)
		if err != nil {
			return retryPostgresError(fmt.Errorf("get all counters: %w", err))
		}
		defer rows.Close()

		currentValues := make(map[string]int64)
		for rows.Next() {
			var name string
			var value int64
			if err := rows.Scan(&name, &value); err != nil {
				return retryPostgresError(fmt.Errorf("scan counter: %w", err))
			}
			currentValues[name] = value
		}
		if err := rows.Err(); err != nil {
			return retryPostgresError(fmt.Errorf("iterate counters: %w", err))
		}

		values = currentValues
		return nil
	})
	if err != nil {
		return nil, err
	}

	return values, nil
}
