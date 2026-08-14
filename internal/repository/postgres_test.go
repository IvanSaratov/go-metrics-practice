package repository

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	models "github.com/IvanSaratov/go-metrics-practice/internal/model"
	"github.com/lib/pq"
	retrylib "github.com/sethvargo/go-retry"
	"github.com/stretchr/testify/require"
)

const (
	setGaugeQueryPattern   = `INSERT INTO gauges \(name, value\) VALUES \(\$1, \$2\) ON CONFLICT \(name\) DO UPDATE SET value = EXCLUDED.value`
	addCounterQueryPattern = `INSERT INTO counters \(name, value\) VALUES \(\$1, \$2\) ON CONFLICT \(name\) DO UPDATE SET value = counters.value \+ EXCLUDED.value RETURNING value`
	addCounterBatchPattern = `INSERT INTO counters \(name, value\) VALUES \(\$1, \$2\) ON CONFLICT \(name\) DO UPDATE SET value = counters.value \+ EXCLUDED.value`
)

func newPostgresStorageMock(t *testing.T) (*PostgresStorage, sqlmock.Sqlmock) {
	t.Helper()

	database, mock, err := sqlmock.New()
	require.NoError(t, err)

	t.Cleanup(func() {
		mock.ExpectClose()
		require.NoError(t, database.Close())
		require.NoError(t, mock.ExpectationsWereMet())
	})

	return NewPostgresStorage(database), mock
}

func TestPostgresStorageSetGauge(t *testing.T) {
	storage, mock := newPostgresStorageMock(t)
	mock.ExpectExec(setGaugeQueryPattern).
		WithArgs("temperature", 23.5).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := storage.SetGauge(context.Background(), "temperature", 23.5)

	require.NoError(t, err)
}

func TestPostgresStorageSetGaugeReturnsDatabaseError(t *testing.T) {
	storage, mock := newPostgresStorageMock(t)
	mock.ExpectExec(setGaugeQueryPattern).
		WithArgs("temperature", 23.5).
		WillReturnError(errors.New("database is unavailable"))

	err := storage.SetGauge(context.Background(), "temperature", 23.5)

	require.ErrorContains(t, err, "set gauge")
}

func TestPostgresStorageSetGaugeRetriesConnectionError(t *testing.T) {
	storage, mock := newPostgresStorageMock(t)
	storage.retry = retryWithoutDelay

	for range 3 {
		mock.ExpectExec(setGaugeQueryPattern).
			WithArgs("temperature", 23.5).
			WillReturnError(&pq.Error{Code: pq.ErrorCode("08006")})
	}
	mock.ExpectExec(setGaugeQueryPattern).
		WithArgs("temperature", 23.5).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := storage.SetGauge(context.Background(), "temperature", 23.5)

	require.NoError(t, err)
}

func TestPostgresStorageSetGaugeDoesNotRetryOtherErrors(t *testing.T) {
	storage, mock := newPostgresStorageMock(t)
	storage.retry = retryWithoutDelay
	mock.ExpectExec(setGaugeQueryPattern).
		WithArgs("temperature", 23.5).
		WillReturnError(&pq.Error{Code: pq.ErrorCode("23505")})

	err := storage.SetGauge(context.Background(), "temperature", 23.5)

	require.ErrorContains(t, err, "set gauge")
}

func TestPostgresStorageAddCounter(t *testing.T) {
	storage, mock := newPostgresStorageMock(t)
	mock.ExpectQuery(addCounterQueryPattern).
		WithArgs("requests", int64(5)).
		WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(int64(17)))

	total, err := storage.AddCounter(context.Background(), "requests", 5)

	require.NoError(t, err)
	require.Equal(t, int64(17), total)
}

func TestPostgresStorageAddCounterReturnsDatabaseError(t *testing.T) {
	storage, mock := newPostgresStorageMock(t)
	mock.ExpectQuery(addCounterQueryPattern).
		WithArgs("requests", int64(5)).
		WillReturnError(errors.New("database is unavailable"))

	_, err := storage.AddCounter(context.Background(), "requests", 5)

	require.ErrorContains(t, err, "add counter")
}

func TestPostgresStorageUpdateBatchCommitsAllMetrics(t *testing.T) {
	storage, mock := newPostgresStorageMock(t)
	mock.ExpectBegin()
	mock.ExpectExec(setGaugeQueryPattern).
		WithArgs("temperature", 23.5).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(addCounterBatchPattern).
		WithArgs("requests", int64(10)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(addCounterBatchPattern).
		WithArgs("requests", int64(5)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := storage.UpdateBatch(context.Background(), []models.Metrics{
		gaugeMetric("temperature", 23.5),
		counterMetric("requests", 10),
		counterMetric("requests", 5),
	})

	require.NoError(t, err)
}

func TestPostgresStorageUpdateBatchRollsBackOnMetricError(t *testing.T) {
	storage, mock := newPostgresStorageMock(t)
	mock.ExpectBegin()
	mock.ExpectExec(setGaugeQueryPattern).
		WithArgs("temperature", 23.5).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(addCounterBatchPattern).
		WithArgs("requests", int64(10)).
		WillReturnError(errors.New("database is unavailable"))
	mock.ExpectRollback()

	err := storage.UpdateBatch(context.Background(), []models.Metrics{
		gaugeMetric("temperature", 23.5),
		counterMetric("requests", 10),
	})

	require.ErrorContains(t, err, "update counter")
}

func TestPostgresStorageUpdateBatchRetriesWholeTransaction(t *testing.T) {
	storage, mock := newPostgresStorageMock(t)
	storage.retry = retryWithoutDelay

	mock.ExpectBegin()
	mock.ExpectExec(setGaugeQueryPattern).
		WithArgs("temperature", 23.5).
		WillReturnError(&pq.Error{Code: pq.ErrorCode("08006")})
	mock.ExpectRollback()

	mock.ExpectBegin()
	mock.ExpectExec(setGaugeQueryPattern).
		WithArgs("temperature", 23.5).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := storage.UpdateBatch(context.Background(), []models.Metrics{
		gaugeMetric("temperature", 23.5),
	})

	require.NoError(t, err)
}

func TestPostgresStorageUpdateBatchRejectsInvalidMetricBeforeTransaction(t *testing.T) {
	storage, _ := newPostgresStorageMock(t)

	err := storage.UpdateBatch(context.Background(), []models.Metrics{
		gaugeMetric("temperature", 23.5),
		{ID: "broken", MType: "unknown"},
	})

	require.ErrorContains(t, err, "unsupported metric type")
}

func TestPostgresStorageUpdateBatchDoesNothingWhenEmpty(t *testing.T) {
	storage, _ := newPostgresStorageMock(t)

	err := storage.UpdateBatch(context.Background(), nil)

	require.NoError(t, err)
}

func TestPostgresStorageGetGauge(t *testing.T) {
	storage, mock := newPostgresStorageMock(t)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT value FROM gauges WHERE name = $1")).
		WithArgs("temperature").
		WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(23.5))

	value, found, err := storage.GetGauge(context.Background(), "temperature")

	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, 23.5, value)
}

func TestPostgresStorageGetGaugeNotFound(t *testing.T) {
	storage, mock := newPostgresStorageMock(t)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT value FROM gauges WHERE name = $1")).
		WithArgs("unknown").
		WillReturnError(sql.ErrNoRows)

	_, found, err := storage.GetGauge(context.Background(), "unknown")

	require.NoError(t, err)
	require.False(t, found)
}

func TestPostgresStorageGetGaugeReturnsDatabaseError(t *testing.T) {
	storage, mock := newPostgresStorageMock(t)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT value FROM gauges WHERE name = $1")).
		WithArgs("temperature").
		WillReturnError(errors.New("database is unavailable"))

	_, _, err := storage.GetGauge(context.Background(), "temperature")

	require.ErrorContains(t, err, "get gauge")
}

func TestPostgresStorageGetCounter(t *testing.T) {
	storage, mock := newPostgresStorageMock(t)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT value FROM counters WHERE name = $1")).
		WithArgs("requests").
		WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(int64(17)))

	value, found, err := storage.GetCounter(context.Background(), "requests")

	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, int64(17), value)
}

func TestPostgresStorageGetCounterNotFound(t *testing.T) {
	storage, mock := newPostgresStorageMock(t)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT value FROM counters WHERE name = $1")).
		WithArgs("unknown").
		WillReturnError(sql.ErrNoRows)

	_, found, err := storage.GetCounter(context.Background(), "unknown")

	require.NoError(t, err)
	require.False(t, found)
}

func TestPostgresStorageGetCounterReturnsDatabaseError(t *testing.T) {
	storage, mock := newPostgresStorageMock(t)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT value FROM counters WHERE name = $1")).
		WithArgs("requests").
		WillReturnError(errors.New("database is unavailable"))

	_, _, err := storage.GetCounter(context.Background(), "requests")

	require.ErrorContains(t, err, "get counter")
}

func TestPostgresStorageGetAllGauges(t *testing.T) {
	storage, mock := newPostgresStorageMock(t)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT name, value FROM gauges")).
		WillReturnRows(sqlmock.NewRows([]string{"name", "value"}).
			AddRow("temperature", 23.5).
			AddRow("pressure", 760.0))

	values, err := storage.GetAllGauges(context.Background())

	require.NoError(t, err)
	require.Equal(t, map[string]float64{
		"temperature": 23.5,
		"pressure":    760.0,
	}, values)
}

func TestPostgresStorageGetAllGaugesReturnsQueryError(t *testing.T) {
	storage, mock := newPostgresStorageMock(t)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT name, value FROM gauges")).
		WillReturnError(errors.New("database is unavailable"))

	_, err := storage.GetAllGauges(context.Background())

	require.ErrorContains(t, err, "get all gauges")
}

func TestPostgresStorageGetAllGaugesReturnsScanError(t *testing.T) {
	storage, mock := newPostgresStorageMock(t)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT name, value FROM gauges")).
		WillReturnRows(sqlmock.NewRows([]string{"name", "value"}).AddRow("temperature", "invalid"))

	_, err := storage.GetAllGauges(context.Background())

	require.ErrorContains(t, err, "scan gauge")
}

func TestPostgresStorageGetAllCounters(t *testing.T) {
	storage, mock := newPostgresStorageMock(t)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT name, value FROM counters")).
		WillReturnRows(sqlmock.NewRows([]string{"name", "value"}).
			AddRow("requests", int64(17)).
			AddRow("errors", int64(2)))

	values, err := storage.GetAllCounters(context.Background())

	require.NoError(t, err)
	require.Equal(t, map[string]int64{
		"requests": 17,
		"errors":   2,
	}, values)
}

func TestPostgresStorageGetAllCountersReturnsQueryError(t *testing.T) {
	storage, mock := newPostgresStorageMock(t)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT name, value FROM counters")).
		WillReturnError(errors.New("database is unavailable"))

	_, err := storage.GetAllCounters(context.Background())

	require.ErrorContains(t, err, "get all counters")
}

func TestPostgresStorageGetAllCountersReturnsScanError(t *testing.T) {
	storage, mock := newPostgresStorageMock(t)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT name, value FROM counters")).
		WillReturnRows(sqlmock.NewRows([]string{"name", "value"}).AddRow("requests", "invalid"))

	_, err := storage.GetAllCounters(context.Background())

	require.ErrorContains(t, err, "scan counter")
}

func retryWithoutDelay(ctx context.Context, operation func(context.Context) error) error {
	backoff := retrylib.WithMaxRetries(3, retrylib.NewConstant(time.Nanosecond))
	return retrylib.Do(ctx, backoff, retrylib.RetryFunc(operation))
}
