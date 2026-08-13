package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/IvanSaratov/go-metrics-practice/internal/repository"
	"github.com/stretchr/testify/require"
)

func TestValueHandlerGetGauge(t *testing.T) {
	storage := repository.NewMemStorage()
	require.NoError(t, storage.SetGauge(context.Background(), "TestGauge", 67.1))
	handler := newTestServer(storage)

	request := httptest.NewRequest(http.MethodGet, "/value/gauge/TestGauge", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "67.1", response.Body.String())
}

func TestValueHandlerGetCounter(t *testing.T) {
	storage := repository.NewMemStorage()
	_, err := storage.AddCounter(context.Background(), "TestCounter", 10)
	require.NoError(t, err)
	handler := newTestServer(storage)

	request := httptest.NewRequest(http.MethodGet, "/value/counter/TestCounter", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "10", response.Body.String())
}

func TestValueHandlerMetricNotFound(t *testing.T) {
	storage := repository.NewMemStorage()
	handler := newTestServer(storage)

	request := httptest.NewRequest(http.MethodGet, "/value/gauge/UnknownGauge", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusNotFound, response.Code)
}

func TestValueHandlerUnknownMetricType(t *testing.T) {
	storage := repository.NewMemStorage()
	handler := newTestServer(storage)

	request := httptest.NewRequest(http.MethodGet, "/value/unknown/TestMetric", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusNotFound, response.Code)
}
