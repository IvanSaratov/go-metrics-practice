package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/IvanSaratov/go-metrics-practice/internal/audit"
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

func newTestHandler(storage repository.Storage, appLogger *zap.Logger, key string) http.Handler {
	server := handler.NewServer(
		storage,
		serverPingFunc(func(context.Context) error { return nil }),
		nil,
	)
	return withMiddleware(server, appLogger, key)
}

func TestServerAppUsesDefaultAddress(t *testing.T) {
	t.Setenv("KEY", "")
	t.Setenv("AUDIT_FILE", "")
	t.Setenv("AUDIT_URL", "")

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
	require.Empty(t, got.key)
	require.Empty(t, got.auditFile)
	require.Empty(t, got.auditURL)
}

func TestServerAppParsesFlags(t *testing.T) {
	t.Setenv("KEY", "")

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
		"-k", "flag-secret",
		"--audit-file", "./logs/audit.jsonl",
		"--audit-url", "http://localhost:9090/audit",
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
	require.Equal(t, "flag-secret", got.key)
	require.Equal(t, "./logs/audit.jsonl", got.auditFile)
	require.Equal(t, "http://localhost:9090/audit", got.auditURL)
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
	t.Setenv("KEY", "env-secret")
	t.Setenv("AUDIT_FILE", "./env/audit.jsonl")
	t.Setenv("AUDIT_URL", "https://example.com/env-audit")
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
	require.Equal(t, "env-secret", got.key)
	require.Equal(t, "./env/audit.jsonl", got.auditFile)
	require.Equal(t, "https://example.com/env-audit", got.auditURL)
}

// Проверяем независимость приёмников и явное переопределение настроек.
func TestServerAppAuditConfiguration(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		envFile  string
		envURL   string
		wantFile string
		wantURL  string
	}{
		{
			name:     "file flag only",
			args:     []string{"--audit-file", "./logs/audit.jsonl"},
			wantFile: "./logs/audit.jsonl",
		},
		{
			name:    "URL flag only",
			args:    []string{"--audit-url", "http://localhost:9090/audit"},
			wantURL: "http://localhost:9090/audit",
		},
		{
			name:     "file environment only",
			envFile:  "./env/audit.jsonl",
			wantFile: "./env/audit.jsonl",
		},
		{
			name:    "URL environment only",
			envURL:  "https://example.com/env-audit",
			wantURL: "https://example.com/env-audit",
		},
		{
			name:     "flags override environment",
			args:     []string{"--audit-file", "./logs/audit.jsonl", "--audit-url", "http://localhost:9090/audit"},
			envFile:  "./env/audit.jsonl",
			envURL:   "https://example.com/env-audit",
			wantFile: "./logs/audit.jsonl",
			wantURL:  "http://localhost:9090/audit",
		},
		{
			name:    "empty file flag overrides environment",
			args:    []string{"--audit-file="},
			envFile: "./env/audit.jsonl",
			envURL:  "https://example.com/env-audit",
			wantURL: "https://example.com/env-audit",
		},
		{
			name:     "empty URL flag overrides environment",
			args:     []string{"--audit-url="},
			envFile:  "./env/audit.jsonl",
			envURL:   "https://example.com/env-audit",
			wantFile: "./env/audit.jsonl",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("AUDIT_FILE", tt.envFile)
			t.Setenv("AUDIT_URL", tt.envURL)

			var got serverConfig
			app := newServerApp(func(config serverConfig) error {
				got = config
				return nil
			})

			err := app.Run(append([]string{"server"}, tt.args...))

			require.NoError(t, err)
			require.Equal(t, tt.wantFile, got.auditFile)
			require.Equal(t, tt.wantURL, got.auditURL)
		})
	}
}

func TestServerAppKeyFlagOverridesEnvironment(t *testing.T) {
	t.Setenv("KEY", "env-secret")
	var got serverConfig
	app := newServerApp(func(config serverConfig) error {
		got = config
		return nil
	})

	err := app.Run([]string{"server", "-k", "flag-secret"})

	require.NoError(t, err)
	require.Equal(t, "flag-secret", got.key)
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
	const key = "secret"

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
	request.Header.Set("HashSHA256", hmacSHA256(t, compressed.Bytes(), key))
	response := httptest.NewRecorder()

	newTestHandler(storage, log, key).ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "gzip", response.Header().Get("Content-Encoding"))
	require.Equal(
		t,
		hmacSHA256(t, response.Body.Bytes(), key),
		response.Header().Get("HashSHA256"),
	)

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
		nil,
	)
	withMiddleware(server, zap.NewNop(), "").ServeHTTP(response, request)

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

	newTestHandler(repository.NewMemStorage(), zap.NewNop(), "").ServeHTTP(response, request)

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

	newTestHandler(repository.NewMemStorage(), zap.NewNop(), "").ServeHTTP(response, request)

	require.Equal(t, http.StatusRequestEntityTooLarge, response.Code)
}

func TestServerHandlerRejectsInvalidSignatureWithoutSavingMetric(t *testing.T) {
	const payload = `{"id":"TestGauge","type":"gauge","value":67.1}`

	storage := repository.NewMemStorage()
	request := httptest.NewRequest(
		http.MethodPost,
		"/update",
		bytes.NewBufferString(payload),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(
		"HashSHA256",
		"0000000000000000000000000000000000000000000000000000000000000000",
	)
	response := httptest.NewRecorder()

	newTestHandler(storage, zap.NewNop(), "secret").ServeHTTP(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
	_, found, err := storage.GetGauge(context.Background(), "TestGauge")
	require.NoError(t, err)
	require.False(t, found)
}

func TestServerHandlerAllowsUnsignedRequestWithKey(t *testing.T) {
	const payload = `{"id":"TestGauge","type":"gauge","value":67.1}`

	storage := repository.NewMemStorage()
	request := httptest.NewRequest(
		http.MethodPost,
		"/update",
		bytes.NewBufferString(payload),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	newTestHandler(storage, zap.NewNop(), "secret").ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	value, found, err := storage.GetGauge(context.Background(), "TestGauge")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, 67.1, value)
	require.NotEmpty(t, response.Header().Get("HashSHA256"))
}

// hmacSHA256 вычисляет ожидаемую подпись независимо от кода приложения.
func hmacSHA256(t *testing.T, data []byte, key string) string {
	t.Helper()

	hash := hmac.New(sha256.New, []byte(key))
	_, err := hash.Write(data)
	require.NoError(t, err)
	return hex.EncodeToString(hash.Sum(nil))
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

// Проверяем выбор приёмников и логирование ошибок без файлов и сетевых соединений.
func TestNewAuditConfiguresReceivers(t *testing.T) {
	fileErr := errors.New("audit write failed")
	clientErr := errors.New("audit connection failed")
	for _, tt := range []struct {
		name       string
		file       string
		url        string
		writeErr   error
		clientErr  error
		wantErrors []error
	}{
		{name: "disabled"},
		{name: "file only", file: "audit.jsonl"},
		{name: "URL only", url: "https://example.com/audit"},
		{name: "both receivers", file: "audit.jsonl", url: "https://example.com/audit"},
		{name: "file failure", file: "audit.jsonl", url: "https://example.com/audit", writeErr: fileErr, wantErrors: []error{fileErr}},
		{name: "URL failure", file: "audit.jsonl", url: "https://example.com/audit", clientErr: clientErr, wantErrors: []error{clientErr}},
		{name: "both failures", file: "audit.jsonl", url: "https://example.com/audit", writeErr: fileErr, clientErr: clientErr, wantErrors: []error{fileErr, clientErr}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			core, logs := observer.New(zapcore.DebugLevel)
			file := &serverAuditFile{writeErr: tt.writeErr}
			opened := false
			openFile := func(path string) (io.WriteCloser, error) {
				opened = true
				require.Equal(t, tt.file, path)
				return file, nil
			}
			var sentEvents []audit.Event
			client := serverAuditHTTPClient(func(request *http.Request) (*http.Response, error) {
				defer request.Body.Close()
				require.Equal(t, tt.url, request.URL.String())
				var event audit.Event
				require.NoError(t, json.NewDecoder(request.Body).Decode(&event))
				sentEvents = append(sentEvents, event)
				if tt.clientErr != nil {
					return nil, tt.clientErr
				}
				return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader(""))}, nil
			})

			notify, closer, err := newAudit(serverConfig{auditFile: tt.file, auditURL: tt.url}, zap.New(core), openFile, client)

			require.NoError(t, err)
			require.Equal(t, tt.file != "", opened)
			if tt.file == "" && tt.url == "" {
				require.Nil(t, notify)
				require.Nil(t, closer)
				require.Empty(t, sentEvents)
				return
			}
			require.NotNil(t, notify)
			event := audit.Event{Timestamp: 12345678, Metrics: []string{"Alloc"}, IPAddress: "192.168.0.42"}
			notify(context.Background(), event)
			if tt.file != "" && tt.writeErr == nil {
				var written audit.Event
				require.NoError(t, json.Unmarshal(file.Bytes(), &written))
				require.Equal(t, event, written)
			} else {
				require.Zero(t, file.Len())
			}
			if tt.url != "" {
				require.Equal(t, []audit.Event{event}, sentEvents)
			} else {
				require.Empty(t, sentEvents)
			}
			entries := logs.FilterLevelExact(zapcore.ErrorLevel).All()
			if len(tt.wantErrors) == 0 {
				require.Empty(t, entries)
			} else {
				require.Len(t, entries, 1)
				for _, wantErr := range tt.wantErrors {
					require.Contains(t, entries[0].ContextMap()["error"], wantErr.Error())
				}
			}
			if tt.file != "" {
				require.NotNil(t, closer)
				require.NoError(t, closer.Close())
				require.True(t, file.closed)
			} else {
				require.Nil(t, closer)
			}
		})
	}
}

// Неверные настройки должны выявляться до открытия порта сервера.
func TestNewAuditRejectsInvalidConfiguration(t *testing.T) {
	openErr := errors.New("audit file unavailable")
	client := serverAuditHTTPClient(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("unexpected HTTP request")
	})
	for _, tt := range []struct {
		name     string
		url      string
		openErr  error
		wantOpen bool
	}{
		{name: "invalid URL", url: "/audit"},
		{name: "file open failure", url: "https://example.com/audit", openErr: openErr, wantOpen: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			opened := false
			notify, closer, err := newAudit(serverConfig{auditFile: "audit.jsonl", auditURL: tt.url}, zap.NewNop(),
				func(string) (io.WriteCloser, error) {
					opened = true
					if tt.openErr != nil {
						return nil, tt.openErr
					}
					return &serverAuditFile{}, nil
				}, client)

			require.Error(t, err)
			if tt.openErr != nil {
				require.ErrorIs(t, err, tt.openErr)
			}
			require.Equal(t, tt.wantOpen, opened)
			require.Nil(t, notify)
			require.Nil(t, closer)
		})
	}
}

// Перенаправление не должно менять POST на GET или отправлять аудит на другой URL.
func TestAuditHTTPClientDoesNotFollowRedirects(t *testing.T) {
	client := newAuditHTTPClient()
	requests := 0
	client.Transport = serverAuditTransport(func(request *http.Request) (*http.Response, error) {
		requests++
		require.Equal(t, http.MethodPost, request.Method)
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": []string{"https://example.com/other"}},
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	})
	request, err := http.NewRequest(http.MethodPost, "https://example.com/audit", strings.NewReader(`{}`))
	require.NoError(t, err)

	response, err := client.Do(request)

	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusFound, response.StatusCode)
	require.Equal(t, 1, requests)
}

// Ошибка адреса аудита должна возвращаться до попытки слушать порт.
func TestRunServerRejectsInvalidAuditBeforeListening(t *testing.T) {
	err := runServer(context.Background(), serverConfig{address: "127.0.0.1:-1", auditURL: "/audit"}, zap.NewNop())

	require.ErrorContains(t, err, "audit URL")
	require.NotContains(t, err.Error(), "listen on")
}

// Сбой обоих приёмников не должен менять сохранённую метрику и успешный ответ.
func TestServerHandlerKeepsSuccessWhenAuditFails(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	appLogger := zap.New(core)
	notify, closer, err := newAudit(serverConfig{
		auditFile: "audit.jsonl",
		auditURL:  "https://example.com/audit",
	}, appLogger, func(string) (io.WriteCloser, error) {
		return &serverAuditFile{writeErr: errors.New("audit write failed")}, nil
	}, serverAuditHTTPClient(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("audit connection failed")
	}))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })
	storage := repository.NewMemStorage()
	server := handler.NewServer(storage, nil, notify)
	request := httptest.NewRequest(http.MethodPost, "/update", strings.NewReader(`{"id":"Alloc","type":"gauge","value":12.5}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	withMiddleware(server, appLogger, "").ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	value, found, err := storage.GetGauge(context.Background(), "Alloc")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, 12.5, value)
	require.Len(t, logs.FilterLevelExact(zapcore.ErrorLevel).All(), 1)
}

type serverAuditFile struct {
	bytes.Buffer
	writeErr error
	closed   bool
}

// Позволяет проверить ошибку записи без настоящего файла.
func (f *serverAuditFile) Write(data []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return f.Buffer.Write(data)
}

// Отмечает освобождение ресурса сборкой сервера.
func (f *serverAuditFile) Close() error {
	f.closed = true
	return nil
}

type serverAuditHTTPClient func(*http.Request) (*http.Response, error)

// Подменяет внешнюю отправку событием в памяти.
func (f serverAuditHTTPClient) Do(request *http.Request) (*http.Response, error) {
	return f(request)
}

type serverAuditTransport func(*http.Request) (*http.Response, error)

// Проверяет политику HTTP-клиента без сетевого соединения.
func (f serverAuditTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
