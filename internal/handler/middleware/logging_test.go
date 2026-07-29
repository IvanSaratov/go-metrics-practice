package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestLoggingMiddlewareLogsCompletedRequest(t *testing.T) {
	core, observedLogs := observer.New(zapcore.DebugLevel)
	log := zap.New(core)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(time.Millisecond)
		w.WriteHeader(http.StatusCreated)
		_, err := w.Write([]byte("payload"))
		require.NoError(t, err)
	})

	request := httptest.NewRequest(
		http.MethodPost,
		"/update/gauge/load/42?source=agent",
		nil,
	)
	response := httptest.NewRecorder()

	LoggingMiddleware(log)(next).ServeHTTP(response, request)

	require.Equal(t, http.StatusCreated, response.Code)
	require.Equal(t, "payload", response.Body.String())

	entries := observedLogs.All()
	require.Len(t, entries, 1)

	entry := entries[0]
	require.Equal(t, zapcore.InfoLevel, entry.Level)
	require.Equal(t, "request completed", entry.Message)

	fields := entry.ContextMap()
	require.Equal(t, http.MethodPost, fields["method"])
	require.Equal(t, "/update/gauge/load/42?source=agent", fields["uri"])
	require.EqualValues(t, http.StatusCreated, fields["status"])
	require.EqualValues(t, len("payload"), fields["size"])

	duration, ok := fields["duration"].(time.Duration)
	require.True(t, ok)
	require.GreaterOrEqual(t, duration, time.Millisecond)
}

func TestLoggingMiddlewareDefaultsEmptyResponseToOK(t *testing.T) {
	core, observedLogs := observer.New(zapcore.DebugLevel)
	log := zap.New(core)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()

	LoggingMiddleware(log)(next).ServeHTTP(response, request)

	entry := requireExactlyOneLogEntry(t, observedLogs)
	fields := entry.ContextMap()
	require.EqualValues(t, http.StatusOK, fields["status"])
	require.EqualValues(t, 0, fields["size"])
}

func TestLoggingMiddlewareAccumulatesResponseSize(t *testing.T) {
	core, observedLogs := observer.New(zapcore.DebugLevel)
	log := zap.New(core)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := w.Write([]byte("first"))
		require.NoError(t, err)
		_, err = w.Write([]byte("-second"))
		require.NoError(t, err)
	})
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	response := httptest.NewRecorder()

	LoggingMiddleware(log)(next).ServeHTTP(response, request)

	entry := requireExactlyOneLogEntry(t, observedLogs)
	fields := entry.ContextMap()
	require.EqualValues(t, http.StatusOK, fields["status"])
	require.EqualValues(t, len("first-second"), fields["size"])
}

func requireExactlyOneLogEntry(t *testing.T, observedLogs *observer.ObservedLogs) observer.LoggedEntry {
	t.Helper()

	entries := observedLogs.All()
	require.Len(t, entries, 1)
	return entries[0]
}
