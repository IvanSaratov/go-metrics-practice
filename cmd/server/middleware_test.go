package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
