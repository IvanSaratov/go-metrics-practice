package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

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
		fileStoragePath: "",
		restore:         true,
	}, zap.NewNop())

	require.NoError(t, err)
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

// Ошибка адреса аудита должна возвращаться до попытки слушать порт.
func TestRunServerRejectsInvalidAuditBeforeListening(t *testing.T) {
	err := runServer(context.Background(), serverConfig{address: "127.0.0.1:-1", auditURL: "/audit"}, zap.NewNop())

	require.ErrorContains(t, err, "audit URL")
	require.NotContains(t, err.Error(), "listen on")
}
