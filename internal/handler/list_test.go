package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/IvanSaratov/go-metrics-practice/internal/repository"
	"github.com/stretchr/testify/require"
)

func TestListHandlerShowsAllMetrics(t *testing.T) {
	storage := repository.NewMemStorage()
	storage.SetGauge("TestGauge", 67.1)
	storage.AddCounter("TestCounter", 10)
	handler := NewRouter(storage)

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Header().Get("Content-Type"), "text/html")
	require.Contains(t, response.Body.String(), "TestGauge")
	require.Contains(t, response.Body.String(), "67.1")
	require.Contains(t, response.Body.String(), "TestCounter")
	require.Contains(t, response.Body.String(), "10")
}
