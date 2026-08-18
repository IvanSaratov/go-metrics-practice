package handler

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	handlermiddleware "github.com/IvanSaratov/go-metrics-practice/internal/handler/middleware"
	models "github.com/IvanSaratov/go-metrics-practice/internal/model"
	"github.com/IvanSaratov/go-metrics-practice/internal/repository"
	"github.com/stretchr/testify/require"
)

func TestUpdatesJSONStoresBatch(t *testing.T) {
	storage := repository.NewMemStorage()
	request := newJSONRequest(t, http.MethodPost, "/updates/", `[
		{"id":"temperature","type":"gauge","value":23.5},
		{"id":"requests","type":"counter","delta":10},
		{"id":"requests","type":"counter","delta":5}
	]`)
	response := httptest.NewRecorder()

	newTestServer(storage).ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "application/json", response.Header().Get("Content-Type"))
	gauge, found, err := storage.GetGauge(context.Background(), "temperature")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, 23.5, gauge)
	counter, found, err := storage.GetCounter(context.Background(), "requests")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, int64(15), counter)

	var metrics []models.Metrics
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &metrics))
	require.Len(t, metrics, 3)
}

func TestUpdatesJSONRejectsInvalidBatchWithoutPartialUpdate(t *testing.T) {
	storage := repository.NewMemStorage()
	request := newJSONRequest(t, http.MethodPost, "/updates/", `[
		{"id":"temperature","type":"gauge","value":23.5},
		{"id":"requests","type":"counter"}
	]`)
	response := httptest.NewRecorder()

	newTestServer(storage).ServeHTTP(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
	_, found, err := storage.GetGauge(context.Background(), "temperature")
	require.NoError(t, err)
	require.False(t, found)
}

func TestUpdatesJSONAcceptsEmptyBatch(t *testing.T) {
	request := newJSONRequest(t, http.MethodPost, "/updates/", `[]`)
	response := httptest.NewRecorder()

	newTestServer(repository.NewMemStorage()).ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.JSONEq(t, `[]`, response.Body.String())
}

func TestUpdatesJSONRejectsNonArray(t *testing.T) {
	request := newJSONRequest(t, http.MethodPost, "/updates/", `{}`)
	response := httptest.NewRecorder()

	newTestServer(repository.NewMemStorage()).ServeHTTP(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
}

func TestUpdatesJSONAcceptsGzipRequest(t *testing.T) {
	storage := repository.NewMemStorage()
	request := newGzipJSONRequest(
		t,
		"/updates/",
		`[{"id":"temperature","type":"gauge","value":23.5}]`,
	)
	response := httptest.NewRecorder()
	handler := handlermiddleware.GzipMiddleware(newTestServer(storage))

	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	value, found, err := storage.GetGauge(context.Background(), "temperature")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, 23.5, value)
}

func newGzipJSONRequest(t *testing.T, target string, body string) *http.Request {
	t.Helper()

	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, err := writer.Write([]byte(body))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	request := httptest.NewRequest(http.MethodPost, target, &compressed)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Content-Encoding", "gzip")
	return request
}

func TestUpdatesJSONRejectsWrongContentType(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/updates/", strings.NewReader(`[]`))
	request.Header.Set("Content-Type", "text/plain")
	response := httptest.NewRecorder()

	newTestServer(repository.NewMemStorage()).ServeHTTP(response, request)

	require.Equal(t, http.StatusUnsupportedMediaType, response.Code)
}
