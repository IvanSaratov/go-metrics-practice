package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/IvanSaratov/go-metrics-practice/internal/handler"
	"github.com/IvanSaratov/go-metrics-practice/internal/repository"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

type serverPingFunc func(context.Context) error

func (f serverPingFunc) PingContext(ctx context.Context) error {
	return f(ctx)
}

func newTestHandler(storage repository.Storage, appLogger *zap.Logger) http.Handler {
	server := handler.NewServer(
		storage,
		serverPingFunc(func(context.Context) error { return nil }),
	)
	return withMiddleware(server, appLogger)
}

func TestServerAppUsesDefaultAddress(t *testing.T) {
	var got serverConfig
	app := newServerApp(func(config serverConfig) error {
		got = config
		return nil
	})

	err := app.Run([]string{"server"})

	require.NoError(t, err)
	require.Equal(t, "localhost:8080", got.address)
	require.Equal(t, 300*time.Second, got.storeInterval)
	require.Equal(t, "./temp/metrics-db.json", got.fileStoragePath)
	require.True(t, got.restore)
	require.Empty(t, got.databaseDSN)
}

func TestServerAppParsesFlags(t *testing.T) {
	var got serverConfig
	app := newServerApp(func(config serverConfig) error {
		got = config
		return nil
	})

	err := app.Run([]string{
		"server",
		"-a", "localhost:9090",
		"-i", "15",
		"-f", "./custom/metrics.json",
		"-r=false",
		"-d", "postgres://flag-user:flag-password@localhost:5432/flag-db?sslmode=disable",
	})

	require.NoError(t, err)
	require.Equal(t, "localhost:9090", got.address)
	require.Equal(t, 15*time.Second, got.storeInterval)
	require.Equal(t, "./custom/metrics.json", got.fileStoragePath)
	require.False(t, got.restore)
	require.Equal(
		t,
		"postgres://flag-user:flag-password@localhost:5432/flag-db?sslmode=disable",
		got.databaseDSN,
	)
}

func TestServerAppParsesEnv(t *testing.T) {
	t.Setenv("ADDRESS", "localhost:9090")
	t.Setenv("STORE_INTERVAL", "20")
	t.Setenv("FILE_STORAGE_PATH", "./env/metrics.json")
	t.Setenv("RESTORE", "false")
	t.Setenv(
		"DATABASE_DSN",
		"postgres://env-user:env-password@localhost:5432/env-db?sslmode=disable",
	)
	var got serverConfig
	app := newServerApp(func(config serverConfig) error {
		got = config
		return nil
	})

	err := app.Run([]string{"server"})

	require.NoError(t, err)
	require.Equal(t, "localhost:9090", got.address)
	require.Equal(t, 20*time.Second, got.storeInterval)
	require.Equal(t, "./env/metrics.json", got.fileStoragePath)
	require.False(t, got.restore)
	require.Equal(
		t,
		"postgres://env-user:env-password@localhost:5432/env-db?sslmode=disable",
		got.databaseDSN,
	)
}

func TestServerAppDatabaseDSNFlagOverridesEnvironment(t *testing.T) {
	t.Setenv(
		"DATABASE_DSN",
		"postgres://env-user:env-password@localhost:5432/env-db?sslmode=disable",
	)
	var got serverConfig
	app := newServerApp(func(config serverConfig) error {
		got = config
		return nil
	})

	err := app.Run([]string{
		"server",
		"-d", "postgres://flag-user:flag-password@localhost:5432/flag-db?sslmode=disable",
	})

	require.NoError(t, err)
	require.Equal(
		t,
		"postgres://flag-user:flag-password@localhost:5432/flag-db?sslmode=disable",
		got.databaseDSN,
	)
}

func TestServerAppParsesDatabaseDSNFlag(t *testing.T) {
	var got serverConfig
	app := newServerApp(func(config serverConfig) error {
		got = config
		return nil
	})

	err := app.Run([]string{
		"server",
		"--database-dsn",
		"postgres://url-user:url-password@localhost:5432/url-db?sslmode=disable",
	})

	require.NoError(t, err)
	require.Equal(
		t,
		"postgres://url-user:url-password@localhost:5432/url-db?sslmode=disable",
		got.databaseDSN,
	)
}

func TestServerAppRejectsNegativeStoreInterval(t *testing.T) {
	app := newServerApp(func(config serverConfig) error {
		return nil
	})

	err := app.Run([]string{"server", "-i", "-1"})

	require.Error(t, err)
}

func TestServerAppRejectsUnknownFlag(t *testing.T) {
	app := newServerApp(func(config serverConfig) error {
		return nil
	})

	err := app.Run([]string{"server", "-unknown"})

	require.Error(t, err)
	require.Contains(t, err.Error(), "flag provided but not defined")
}

func TestRunServerReturnsListenError(t *testing.T) {
	core, _ := observer.New(zapcore.DebugLevel)
	log := zap.New(core)

	err := runServer(context.Background(), serverConfig{address: "127.0.0.1:-1"}, log)

	require.Error(t, err)
	require.ErrorContains(t, err, "listen on 127.0.0.1:-1")
}

func TestRunServerRejectsInvalidDatabaseDSNBeforeListening(t *testing.T) {
	err := runServer(context.Background(), serverConfig{
		address:     "127.0.0.1:-1",
		databaseDSN: "postgres://%",
	}, zap.NewNop())

	require.Error(t, err)
	require.ErrorContains(t, err, "open database")
	require.NotContains(t, err.Error(), "listen on")
}

func TestRunServerRestoresBeforeListening(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics-db.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"invalid":`), 0o600))

	err := runServer(context.Background(), serverConfig{
		address:         "127.0.0.1:-1",
		storeInterval:   300 * time.Second,
		fileStoragePath: path,
		restore:         true,
	}, zap.NewNop())

	require.Error(t, err)
	require.ErrorContains(t, err, "restore metrics")
	require.NotContains(t, err.Error(), "listen on")
}

func TestNewStorageUsesPostgresWhenDatabaseProvided(t *testing.T) {
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		mock.ExpectClose()
		require.NoError(t, database.Close())
		require.NoError(t, mock.ExpectationsWereMet())
	})

	storage, err := newStorage(serverConfig{
		fileStoragePath: filepath.Join(t.TempDir(), "metrics-db.json"),
		restore:         true,
	}, database)

	require.NoError(t, err)
	require.IsType(t, &repository.PostgresStorage{}, storage)
}

func TestNewStorageUsesFileWhenPathProvided(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics-db.json")
	storage, err := newStorage(serverConfig{
		fileStoragePath: path,
		storeInterval:   0,
	}, nil)
	require.NoError(t, err)

	require.NoError(t, storage.SetGauge(context.Background(), "temperature", 23.5))
	restored := repository.NewFileStorage(path, false)
	require.NoError(t, restored.Restore())
	value, found, err := restored.GetGauge(context.Background(), "temperature")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, 23.5, value)
}

func TestNewStorageUsesMemoryWhenFilePathEmpty(t *testing.T) {
	storage, err := newStorage(serverConfig{fileStoragePath: " \t "}, nil)

	require.NoError(t, err)
	require.IsType(t, &repository.MemStorage{}, storage)
}

func TestRunServerUsesMemoryWithoutDatabaseOrFile(t *testing.T) {
	t.Chdir(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := runServer(ctx, serverConfig{
		address:         "127.0.0.1:0",
		fileStoragePath: "",
		restore:         true,
	}, zap.NewNop())

	require.NoError(t, err)
}

func TestSaveMetricsPeriodicallyWritesOnTick(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics-db.json")
	storage := repository.NewFileStorage(path, false)
	require.NoError(t, storage.SetGauge(context.Background(), "TestGauge", 67.1))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time)
	done := make(chan struct{})
	go func() {
		defer close(done)
		saveMetricsPeriodically(ctx, ticks, storage, zap.NewNop())
	}()

	ticks <- time.Now()
	close(ticks)
	<-done

	restored := repository.NewFileStorage(path, false)
	require.NoError(t, restored.Restore())
	value, ok, err := restored.GetGauge(context.Background(), "TestGauge")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 67.1, value)
}

func TestServerHandlerSupportsGzip(t *testing.T) {
	metricID := strings.Repeat("TestGauge", 20)
	payload := []byte(`{"id":"` + metricID + `","type":"gauge","value":67.1}`)
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, err := writer.Write(payload)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	core, observedLogs := observer.New(zapcore.DebugLevel)
	log := zap.New(core)
	storage := repository.NewMemStorage()
	request := httptest.NewRequest(http.MethodPost, "/update", &compressed)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Content-Encoding", "gzip")
	request.Header.Set("Accept-Encoding", "gzip")
	response := httptest.NewRecorder()

	newTestHandler(storage, log).ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "gzip", response.Header().Get("Content-Encoding"))

	reader, err := gzip.NewReader(bytes.NewReader(response.Body.Bytes()))
	require.NoError(t, err)
	body, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	require.JSONEq(t, string(payload), string(body))

	value, ok, err := storage.GetGauge(context.Background(), metricID)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 67.1, value)

	entries := observedLogs.All()
	require.Len(t, entries, 1)
	require.EqualValues(t, response.Body.Len(), entries[0].ContextMap()["size"])
}

func TestServerHandlerProvidesDatabasePing(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/ping", nil)
	response := httptest.NewRecorder()

	server := handler.NewServer(
		repository.NewMemStorage(),
		serverPingFunc(func(context.Context) error { return nil }),
	)
	withMiddleware(server, zap.NewNop()).ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
}

func TestServerHandlerRejectsCorruptedGzipRequest(t *testing.T) {
	payload := []byte(`{"id":"TestGauge","type":"gauge","value":67.1}`)
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, err := writer.Write(payload)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	body := compressed.Bytes()
	body[len(body)-1] ^= 0xff

	request := httptest.NewRequest(http.MethodPost, "/update", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Content-Encoding", "gzip")
	response := httptest.NewRecorder()

	newTestHandler(repository.NewMemStorage(), zap.NewNop()).ServeHTTP(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
}

func TestServerHandlerLimitsDecompressedRequest(t *testing.T) {
	payload := []byte(`{"id":"` + strings.Repeat("x", (1<<20)+1) + `","type":"gauge","value":67.1}`)
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, err := writer.Write(payload)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	request := httptest.NewRequest(http.MethodPost, "/update", &compressed)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Content-Encoding", "gzip")
	response := httptest.NewRecorder()

	newTestHandler(repository.NewMemStorage(), zap.NewNop()).ServeHTTP(response, request)

	require.Equal(t, http.StatusRequestEntityTooLarge, response.Code)
}

func TestRunLogsApplicationError(t *testing.T) {
	core, observedLogs := observer.New(zapcore.DebugLevel)
	log := zap.New(core)

	exitCode := run([]string{"server", "-unknown"}, log)

	require.Equal(t, 1, exitCode)

	errorEntries := observedLogs.FilterLevelExact(zapcore.ErrorLevel).All()
	require.Len(t, errorEntries, 1)
	require.Equal(t, "server failed", errorEntries[0].Message)
	require.Contains(t, errorEntries[0].ContextMap(), "error")
}

func TestRunServerShutsDownWhenContextCancelled(t *testing.T) {
	core, observedLogs := observer.New(zapcore.DebugLevel)
	log := zap.New(core)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	storagePath := filepath.Join(t.TempDir(), "metrics-db.json")

	err := runServer(ctx, serverConfig{
		address:         "127.0.0.1:0",
		storeInterval:   300 * time.Second,
		fileStoragePath: storagePath,
		restore:         false,
	}, log)

	require.NoError(t, err)
	_, err = os.Stat(storagePath)
	require.NoError(t, err)
	infoEntries := observedLogs.FilterLevelExact(zapcore.InfoLevel).All()
	messages := make([]string, 0, len(infoEntries))
	for _, entry := range infoEntries {
		messages = append(messages, entry.Message)
	}
	require.Equal(t, []string{
		"server started",
		"shutdown requested",
		"server stopped",
	}, messages)
}
