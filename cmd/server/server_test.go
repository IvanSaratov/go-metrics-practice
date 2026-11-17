package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/IvanSaratov/go-metrics-practice/internal/audit"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

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

func TestRunServerRejectsInvalidCryptoKeyBeforeListening(t *testing.T) {
	invalidKeyPath := filepath.Join(t.TempDir(), "invalid.pem")
	require.NoError(t, os.WriteFile(invalidKeyPath, []byte("not a PEM key"), 0o600))
	for _, tt := range []struct {
		name        string
		keyPath     string
		databaseDSN string
	}{
		{"missing key", filepath.Join(t.TempDir(), "missing.pem"), ""},
		{"invalid key", invalidKeyPath, ""},
		{"key checked before database", invalidKeyPath, "postgres://%"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := runServer(context.Background(), serverConfig{
				address:       "127.0.0.1:-1",
				cryptoKeyPath: tt.keyPath,
				databaseDSN:   tt.databaseDSN,
			}, zap.NewNop())

			require.ErrorContains(t, err, "load private key")
			require.NotContains(t, err.Error(), "listen on")
			require.NotContains(t, err.Error(), "open database")
		})
	}
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

func TestRunServerUsesMemoryWithoutDatabaseOrFile(t *testing.T) {
	t.Chdir(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := runServer(ctx, serverConfig{
		address:         "127.0.0.1:0",
		shutdownTimeout: time.Second,
		fileStoragePath: "",
		restore:         true,
	}, zap.NewNop())

	require.NoError(t, err)
}

func TestRunServerShutsDownWhenContextCancelled(t *testing.T) {
	core, observedLogs := observer.New(zapcore.DebugLevel)
	log := zap.New(core)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	storagePath := filepath.Join(t.TempDir(), "metrics-db.json")

	err := runServer(ctx, serverConfig{
		address:         "127.0.0.1:0",
		shutdownTimeout: time.Second,
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

func TestRunServerShutsDownWithActiveRequest(t *testing.T) {
	for _, tt := range []struct {
		name         string
		completeBody bool
		timeout      time.Duration
	}{
		{"request completes", true, 5 * time.Second},
		{"request stalls", false, 20 * time.Millisecond},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			config := serverConfig{
				address: "127.0.0.1:0", shutdownTimeout: tt.timeout,
				storeInterval: time.Hour, fileStoragePath: filepath.Join(dir, "metrics.json"),
				auditFile: filepath.Join(dir, "audit.jsonl"),
			}
			core, logs := observer.New(zap.InfoLevel)
			ctx, stop := context.WithCancel(t.Context())
			defer stop()
			done := make(chan error, 1)
			go func() { done <- runServer(ctx, config, zap.New(core)) }()

			waitForLog := func(message string) observer.LoggedEntry {
				t.Helper()
				require.Eventually(t, func() bool {
					return logs.FilterMessage(message).Len() != 0
				}, time.Second, time.Millisecond)
				return logs.FilterMessage(message).All()[0]
			}
			address := waitForLog("server started").ContextMap()["address"].(string)
			conn, err := net.DialTimeout("tcp", address, time.Second)
			require.NoError(t, err)
			defer conn.Close()
			require.NoError(t, conn.SetDeadline(time.Now().Add(3*time.Second)))

			// Ответ 100 Continue подтверждает, что обработчик уже ждёт тело запроса.
			body := `{"id":"Alloc","type":"gauge","value":67.1}`
			_, err = fmt.Fprintf(conn, "POST /update HTTP/1.1\r\nHost: localhost\r\nContent-Type: application/json\r\nContent-Length: %d\r\nExpect: 100-continue\r\n\r\n", len(body))
			require.NoError(t, err)
			reader := bufio.NewReader(conn)
			resp, err := http.ReadResponse(reader, nil)
			require.NoError(t, err)
			resp.Body.Close()
			require.Equal(t, http.StatusContinue, resp.StatusCode)

			stop()
			if tt.completeBody {
				waitForLog("shutdown requested")
				_, err = io.WriteString(conn, body)
				require.NoError(t, err)
				resp, err = http.ReadResponse(reader, nil)
				require.NoError(t, err)
				resp.Body.Close()
				require.Equal(t, http.StatusOK, resp.StatusCode)
			}
			select {
			case err := <-done:
				if !tt.completeBody {
					require.ErrorIs(t, err, context.DeadlineExceeded)
					_, err = reader.ReadByte()
					require.ErrorIs(t, err, io.EOF)
					return
				}
				require.NoError(t, err)
			case <-time.After(2 * time.Second):
				t.Fatal("server did not stop")
			}

			metrics, err := os.ReadFile(config.fileStoragePath)
			require.NoError(t, err)
			require.JSONEq(t, "["+body+"]", string(metrics))
			data, err := os.ReadFile(config.auditFile)
			require.NoError(t, err)
			var event audit.Event
			require.NoError(t, json.Unmarshal(data, &event))
			require.Equal(t, []string{"Alloc"}, event.Metrics)
		})
	}
}

func TestRunServerReturnsFinalSaveError(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := runServer(ctx, serverConfig{
		address:         "127.0.0.1:0",
		shutdownTimeout: time.Second,
		storeInterval:   time.Hour,
		fileStoragePath: t.TempDir(), // Каталог нельзя заменить файлом с метриками.
	}, zap.NewNop())

	require.ErrorContains(t, err, "save metrics on shutdown")
}

func TestRunServerRejectsInvalidAuditBeforeListening(t *testing.T) {
	err := runServer(context.Background(), serverConfig{address: "127.0.0.1:-1", auditURL: "/audit"}, zap.NewNop())

	require.ErrorContains(t, err, "audit URL")
	require.NotContains(t, err.Error(), "listen on")
}
