package handler

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/IvanSaratov/go-metrics-practice/internal/cryptoutil"
	"github.com/IvanSaratov/go-metrics-practice/internal/repository"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

type pingFunc func(context.Context) error

func (f pingFunc) PingContext(ctx context.Context) error {
	return f(ctx)
}

func newTestServer(storage repository.Storage) *Server {
	return NewServer(
		storage,
		pingFunc(func(context.Context) error { return nil }),
		Config{},
	)
}

func TestServerHandlerSupportsGzip(t *testing.T) {
	const key = "secret"
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	for _, encrypted := range []bool{false, true} {
		t.Run(fmt.Sprintf("encrypted=%t", encrypted), func(t *testing.T) {
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
			body := compressed.Bytes()
			encoding := "gzip"
			if encrypted {
				body, err = cryptoutil.Encrypt(&privateKey.PublicKey, body)
				require.NoError(t, err)
				encoding = "gzip, rsa-aesgcm"
			}
			request := httptest.NewRequest(http.MethodPost, "/update/", bytes.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Content-Encoding", encoding)
			request.Header.Set("Accept-Encoding", "gzip")
			request.Header.Set("HashSHA256", hmacSHA256(t, body, key))
			response := httptest.NewRecorder()

			NewServer(storage, nil, Config{Logger: log, SigningKey: key, PrivateKey: privateKey}).ServeHTTP(response, request)

			require.Equal(t, http.StatusOK, response.Code)
			require.Equal(t, "gzip", response.Header().Get("Content-Encoding"))
			require.Equal(
				t,
				hmacSHA256(t, response.Body.Bytes(), key),
				response.Header().Get("HashSHA256"),
			)

			reader, err := gzip.NewReader(bytes.NewReader(response.Body.Bytes()))
			require.NoError(t, err)
			responseBody, err := io.ReadAll(reader)
			require.NoError(t, err)
			require.NoError(t, reader.Close())
			require.JSONEq(t, string(payload), string(responseBody))

			value, ok, err := storage.GetGauge(context.Background(), metricID)
			require.NoError(t, err)
			require.True(t, ok)
			require.Equal(t, 67.1, value)

			entries := observedLogs.All()
			require.Len(t, entries, 1)
			require.EqualValues(t, response.Body.Len(), entries[0].ContextMap()["size"])
		})
	}
}

func TestServerHandlerRejectsEncryptedRequestWithoutKey(t *testing.T) {
	for _, encodings := range [][]string{
		{"gzip, " + cryptoutil.Encoding},
		{"identity", cryptoutil.Encoding},
	} {
		t.Run(strings.Join(encodings, ";"), func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/update", strings.NewReader(`{"id":"Alloc","type":"gauge","value":3}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header["Content-Encoding"] = encodings
			response := httptest.NewRecorder()

			newTestServer(repository.NewMemStorage()).ServeHTTP(response, request)

			require.Equal(t, http.StatusUnsupportedMediaType, response.Code)
		})
	}
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

	newTestServer(repository.NewMemStorage()).ServeHTTP(response, request)

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

	newTestServer(repository.NewMemStorage()).ServeHTTP(response, request)

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

	NewServer(storage, nil, Config{SigningKey: "secret"}).ServeHTTP(response, request)

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

	NewServer(storage, nil, Config{SigningKey: "secret"}).ServeHTTP(response, request)

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
