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
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/IvanSaratov/go-metrics-practice/internal/audit"
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
	_, subnet, err := net.ParseCIDR("192.168.1.0/24")
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
			request.Header.Set("X-Real-IP", "192.168.1.5")
			response := httptest.NewRecorder()

			NewServer(storage, nil, Config{Logger: log, SigningKey: key, PrivateKey: privateKey, TrustedSubnet: subnet}).ServeHTTP(response, request)

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

func TestServerTrustedSubnetProtectsAllUpdateRoutes(t *testing.T) {
	_, subnet, err := net.ParseCIDR("192.168.1.0/24")
	require.NoError(t, err)
	const metric = `{"id":"PollCount","type":"counter","delta":5}`
	for _, route := range []struct {
		path string
		body string
	}{
		{path: "/update", body: metric},
		{path: "/update/", body: metric},
		{path: "/updates", body: "[" + metric + "]"},
		{path: "/updates/", body: "[" + metric + "]"},
		{path: "/update/counter/PollCount/5"},
		{path: "/update/counter/PollCount/5/"},
	} {
		for _, tt := range []struct {
			name     string
			ip       string
			disabled bool
			allowed  bool
		}{
			{name: "trusted", ip: "192.168.1.5", allowed: true},
			{name: "untrusted", ip: "10.0.0.1"},
			{name: "missing header"},
			{name: "disabled without header", disabled: true, allowed: true},
			{name: "disabled with invalid IP", ip: "invalid", disabled: true, allowed: true},
		} {
			t.Run(route.path+"/"+tt.name, func(t *testing.T) {
				storage := repository.NewMemStorage()
				_, err := storage.AddCounter(t.Context(), "PollCount", 10)
				require.NoError(t, err)
				auditCalled := false
				config := Config{
					TrustedSubnet: subnet,
					NotifyAudit:   func(context.Context, audit.Event) { auditCalled = true },
				}
				if tt.disabled {
					config.TrustedSubnet = nil
				}
				request := httptest.NewRequest(http.MethodPost, route.path, strings.NewReader(route.body))
				request.Header.Set("Content-Type", "application/json")
				if tt.ip != "" {
					request.Header.Set("X-Real-IP", tt.ip)
				}
				response := httptest.NewRecorder()

				NewServer(storage, nil, config).ServeHTTP(response, request)

				value, found, err := storage.GetCounter(t.Context(), "PollCount")
				require.NoError(t, err)
				require.True(t, found)
				require.Equal(t, tt.allowed, auditCalled)
				if tt.allowed {
					require.Equal(t, http.StatusOK, response.Code)
					require.Equal(t, int64(15), value)
				} else {
					require.Equal(t, http.StatusForbidden, response.Code)
					require.Equal(t, int64(10), value)
				}
			})
		}
	}
}

func TestServerTrustedSubnetDoesNotRestrictReads(t *testing.T) {
	_, subnet, err := net.ParseCIDR("192.168.1.0/24")
	require.NoError(t, err)
	storage := repository.NewMemStorage()
	require.NoError(t, storage.SetGauge(t.Context(), "Alloc", 67.1))
	server := NewServer(storage, pingFunc(func(context.Context) error { return nil }), Config{TrustedSubnet: subnet})

	for _, tt := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/"},
		{method: http.MethodGet, path: "/ping"},
		{method: http.MethodGet, path: "/value/gauge/Alloc"},
		{method: http.MethodPost, path: "/value", body: `{"id":"Alloc","type":"gauge"}`},
	} {
		t.Run(tt.path, func(t *testing.T) {
			request := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-Real-IP", "10.0.0.1")
			response := httptest.NewRecorder()

			server.ServeHTTP(response, request)

			require.Equal(t, http.StatusOK, response.Code)
		})
	}
}

func TestServerRejectsUntrustedBeforeBodyProcessing(t *testing.T) {
	_, subnet, err := net.ParseCIDR("192.168.1.0/24")
	require.NoError(t, err)
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	server := NewServer(repository.NewMemStorage(), nil, Config{
		TrustedSubnet: subnet,
		SigningKey:    "secret",
		PrivateKey:    privateKey,
	})
	for _, tt := range []struct {
		name   string
		header string
		value  string
	}{
		{name: "signature", header: "HashSHA256", value: "invalid"},
		{name: "gzip", header: "Content-Encoding", value: "gzip"},
		{name: "encryption", header: "Content-Encoding", value: "gzip, rsa-aesgcm"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/updates/", strings.NewReader("invalid body"))
			request.Header.Set("X-Real-IP", "10.0.0.1")
			request.Header.Set(tt.header, tt.value)
			response := httptest.NewRecorder()

			server.ServeHTTP(response, request)

			require.Equal(t, http.StatusForbidden, response.Code)
		})
	}
}

// hmacSHA256 вычисляет ожидаемую подпись независимо от кода приложения.
func hmacSHA256(t *testing.T, data []byte, key string) string {
	t.Helper()

	hash := hmac.New(sha256.New, []byte(key))
	_, err := hash.Write(data)
	require.NoError(t, err)
	return hex.EncodeToString(hash.Sum(nil))
}
