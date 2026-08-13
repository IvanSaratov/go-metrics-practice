package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/IvanSaratov/go-metrics-practice/internal/repository"
	"github.com/stretchr/testify/require"
)

func TestUpdateHandlerUpdateGauge(t *testing.T) {
	storage := repository.NewMemStorage()
	handler := newTestServer(storage)

	request := httptest.NewRequest(http.MethodPost, "/update/gauge/TestGauge/67.1", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)

	value, ok := storage.GetGauge("TestGauge")
	require.True(t, ok)
	require.Equal(t, 67.1, value)
}

func TestUpdateHandlerUpdateCounter(t *testing.T) {
	storage := repository.NewMemStorage()
	handler := newTestServer(storage)

	request := httptest.NewRequest(http.MethodPost, "/update/counter/TestCounter/10", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)

	value, ok := storage.GetCounter("TestCounter")
	require.True(t, ok)
	require.Equal(t, int64(10), value)
}

func TestUpdateHandlerUnknownMetricType(t *testing.T) {
	storage := repository.NewMemStorage()
	handler := newTestServer(storage)

	request := httptest.NewRequest(http.MethodPost, "/update/unknown/TestMetric/10", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
}

func TestUpdateHandlerInvalidGaugeValue(t *testing.T) {
	storage := repository.NewMemStorage()
	handler := newTestServer(storage)

	request := httptest.NewRequest(http.MethodPost, "/update/gauge/TestGauge/not-a-number", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
}

func TestUpdateHandlerRejectsNonFiniteGauge(t *testing.T) {
	for _, value := range []string{"NaN", "+Inf", "-Inf"} {
		t.Run(value, func(t *testing.T) {
			storage := repository.NewMemStorage()
			handler := newTestServer(storage)
			request := httptest.NewRequest(
				http.MethodPost,
				"/update/gauge/TestGauge/"+value,
				nil,
			)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			require.Equal(t, http.StatusBadRequest, response.Code)
			_, ok := storage.GetGauge("TestGauge")
			require.False(t, ok)
		})
	}
}

func TestUpdateHandlerInvalidCounterValue(t *testing.T) {
	storage := repository.NewMemStorage()
	handler := newTestServer(storage)

	request := httptest.NewRequest(http.MethodPost, "/update/counter/TestCounter/1.5", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
}

func TestUpdateHandlerEmptyMetricName(t *testing.T) {
	storage := repository.NewMemStorage()
	handler := newTestServer(storage)

	request := httptest.NewRequest(http.MethodPost, "/update/gauge//67.1", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusNotFound, response.Code)
}

func TestUpdateHandlerMissingMetricValue(t *testing.T) {
	storage := repository.NewMemStorage()
	handler := newTestServer(storage)

	request := httptest.NewRequest(http.MethodPost, "/update/gauge/TestGauge", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusNotFound, response.Code)
}

func TestUpdateHandlerUnsupportedMethod(t *testing.T) {
	storage := repository.NewMemStorage()
	handler := newTestServer(storage)

	request := httptest.NewRequest(http.MethodGet, "/update/gauge/TestGauge/67.1", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusMethodNotAllowed, response.Code)
}

func TestUpdateHandlerUnknownPath(t *testing.T) {
	storage := repository.NewMemStorage()
	handler := newTestServer(storage)

	request := httptest.NewRequest(http.MethodPost, "/updater/counter/TestCounter/10", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusNotFound, response.Code)
}
