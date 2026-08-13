package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	models "github.com/IvanSaratov/go-metrics-practice/internal/model"
	"github.com/IvanSaratov/go-metrics-practice/internal/repository"
	"github.com/stretchr/testify/require"
)

func TestUpdateJSONGauge(t *testing.T) {
	storage := repository.NewMemStorage()
	router := NewRouter(storage)
	request := newJSONRequest(t, http.MethodPost, "/update", `{
		"id": "TestGauge",
		"type": "gauge",
		"value": 67.1
	}`)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "application/json", response.Header().Get("Content-Type"))

	value, ok := storage.GetGauge("TestGauge")
	require.True(t, ok)
	require.Equal(t, 67.1, value)

	metric := decodeMetricResponse(t, response)
	require.Equal(t, "TestGauge", metric.ID)
	require.Equal(t, models.Gauge, metric.MType)
	require.NotNil(t, metric.Value)
	require.Equal(t, 67.1, *metric.Value)
	require.Nil(t, metric.Delta)
}

func TestUpdateJSONCounterReturnsAccumulatedValue(t *testing.T) {
	storage := repository.NewMemStorage()
	storage.AddCounter("TestCounter", 7)
	router := NewRouter(storage)
	request := newJSONRequest(t, http.MethodPost, "/update", `{
		"id": "TestCounter",
		"type": "counter",
		"delta": 3
	}`)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "application/json", response.Header().Get("Content-Type"))

	value, ok := storage.GetCounter("TestCounter")
	require.True(t, ok)
	require.Equal(t, int64(10), value)

	metric := decodeMetricResponse(t, response)
	require.Equal(t, "TestCounter", metric.ID)
	require.Equal(t, models.Counter, metric.MType)
	require.NotNil(t, metric.Delta)
	require.Equal(t, int64(10), *metric.Delta)
	require.Nil(t, metric.Value)
}

func TestValueJSONGauge(t *testing.T) {
	storage := repository.NewMemStorage()
	storage.SetGauge("TestGauge", 67.1)
	router := NewRouter(storage)
	request := newJSONRequest(
		t,
		http.MethodPost,
		"/value",
		`{"id":"TestGauge","type":"gauge"}`,
	)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "application/json", response.Header().Get("Content-Type"))

	metric := decodeMetricResponse(t, response)
	require.Equal(t, "TestGauge", metric.ID)
	require.Equal(t, models.Gauge, metric.MType)
	require.NotNil(t, metric.Value)
	require.Equal(t, 67.1, *metric.Value)
	require.Nil(t, metric.Delta)
}

func TestValueJSONCounter(t *testing.T) {
	storage := repository.NewMemStorage()
	storage.AddCounter("TestCounter", 10)
	router := NewRouter(storage)
	request := newJSONRequest(
		t,
		http.MethodPost,
		"/value",
		`{"id":"TestCounter","type":"counter"}`,
	)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "application/json", response.Header().Get("Content-Type"))

	metric := decodeMetricResponse(t, response)
	require.Equal(t, "TestCounter", metric.ID)
	require.Equal(t, models.Counter, metric.MType)
	require.NotNil(t, metric.Delta)
	require.Equal(t, int64(10), *metric.Delta)
	require.Nil(t, metric.Value)
}

func TestUpdateJSONAcceptsTrailingSlash(t *testing.T) {
	storage := repository.NewMemStorage()
	router := NewRouter(storage)
	request := newJSONRequest(
		t,
		http.MethodPost,
		"/update/",
		`{"id":"TestGauge","type":"gauge","value":67.1}`,
	)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "application/json", response.Header().Get("Content-Type"))

	value, ok := storage.GetGauge("TestGauge")
	require.True(t, ok)
	require.Equal(t, 67.1, value)

	metric := decodeMetricResponse(t, response)
	require.Equal(t, "TestGauge", metric.ID)
	require.Equal(t, models.Gauge, metric.MType)
	require.NotNil(t, metric.Value)
	require.Equal(t, 67.1, *metric.Value)
	require.Nil(t, metric.Delta)
}

func TestValueJSONAcceptsTrailingSlash(t *testing.T) {
	storage := repository.NewMemStorage()
	storage.SetGauge("TestGauge", 67.1)
	router := NewRouter(storage)
	request := newJSONRequest(
		t,
		http.MethodPost,
		"/value/",
		`{"id":"TestGauge","type":"gauge"}`,
	)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "application/json", response.Header().Get("Content-Type"))

	metric := decodeMetricResponse(t, response)
	require.Equal(t, "TestGauge", metric.ID)
	require.Equal(t, models.Gauge, metric.MType)
	require.NotNil(t, metric.Value)
	require.Equal(t, 67.1, *metric.Value)
	require.Nil(t, metric.Delta)
}

func TestUpdateJSONValidatesRequest(t *testing.T) {
	largeBody := `{"id":"` + strings.Repeat("a", 1<<20) + `","type":"gauge","value":1}`

	tests := []struct {
		name        string
		contentType string
		body        string
		wantStatus  int
	}{
		{
			name:        "wrong content type",
			contentType: "text/plain",
			body:        `{"id":"TestGauge","type":"gauge","value":1}`,
			wantStatus:  http.StatusUnsupportedMediaType,
		},
		{
			name:        "empty body",
			contentType: "application/json",
			body:        "",
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "missing id",
			contentType: "application/json",
			body:        `{"type":"gauge","value":1}`,
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "missing type",
			contentType: "application/json",
			body:        `{"id":"TestGauge","value":1}`,
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "unknown field",
			contentType: "application/json",
			body:        `{"id":"TestGauge","type":"gauge","value":1,"unknown":true}`,
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "multiple JSON values",
			contentType: "application/json",
			body:        `{"id":"TestGauge","type":"gauge","value":1} {}`,
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "body too large",
			contentType: "application/json",
			body:        largeBody,
			wantStatus:  http.StatusRequestEntityTooLarge,
		},
		{
			name:        "gauge without value",
			contentType: "application/json",
			body:        `{"id":"TestGauge","type":"gauge"}`,
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "counter without delta",
			contentType: "application/json",
			body:        `{"id":"TestCounter","type":"counter"}`,
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "gauge with delta",
			contentType: "application/json",
			body:        `{"id":"TestGauge","type":"gauge","value":1,"delta":1}`,
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "counter with value",
			contentType: "application/json",
			body:        `{"id":"TestCounter","type":"counter","delta":1,"value":1}`,
			wantStatus:  http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := repository.NewMemStorage()
			router := NewRouter(storage)
			request := httptest.NewRequest(
				http.MethodPost,
				"/update",
				strings.NewReader(tt.body),
			)
			request.Header.Set("Content-Type", tt.contentType)
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			require.Equal(t, tt.wantStatus, response.Code)
			require.Equal(t, "application/json", response.Header().Get("Content-Type"))

			var body errorResponse
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			require.NotEmpty(t, body.Error)
		})
	}
}

func TestValueJSONReadsGaugeStoredThroughLegacyEndpoint(t *testing.T) {
	router := NewRouter(repository.NewMemStorage())
	updateRequest := httptest.NewRequest(
		http.MethodPost,
		"/update/gauge/TestGauge/67.1",
		nil,
	)
	updateResponse := httptest.NewRecorder()
	router.ServeHTTP(updateResponse, updateRequest)
	require.Equal(t, http.StatusOK, updateResponse.Code)

	valueRequest := newJSONRequest(
		t,
		http.MethodPost,
		"/value",
		`{"id":"TestGauge","type":"gauge"}`,
	)
	valueResponse := httptest.NewRecorder()

	router.ServeHTTP(valueResponse, valueRequest)

	require.Equal(t, http.StatusOK, valueResponse.Code)
	require.Equal(t, "application/json", valueResponse.Header().Get("Content-Type"))

	metric := decodeMetricResponse(t, valueResponse)
	require.Equal(t, "TestGauge", metric.ID)
	require.Equal(t, models.Gauge, metric.MType)
	require.NotNil(t, metric.Value)
	require.Equal(t, 67.1, *metric.Value)
	require.Nil(t, metric.Delta)
}

func TestUpdateJSONPreservesZeroValues(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "zero gauge",
			body: `{"id":"ZeroGauge","type":"gauge","value":0}`,
		},
		{
			name: "zero counter",
			body: `{"id":"ZeroCounter","type":"counter","delta":0}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := NewRouter(repository.NewMemStorage())
			request := newJSONRequest(t, http.MethodPost, "/update", tt.body)
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			require.Equal(t, http.StatusOK, response.Code)
			metric := decodeMetricResponse(t, response)
			switch metric.MType {
			case models.Gauge:
				require.NotNil(t, metric.Value)
				require.Zero(t, *metric.Value)
			case models.Counter:
				require.NotNil(t, metric.Delta)
				require.Zero(t, *metric.Delta)
			}
		})
	}
}

func TestUpdateJSONAcceptsContentTypeParameters(t *testing.T) {
	router := NewRouter(repository.NewMemStorage())
	request := newJSONRequest(
		t,
		http.MethodPost,
		"/update",
		`{"id":"TestGauge","type":"gauge","value":1}`,
	)
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
}

func TestValueJSONMetricNotFound(t *testing.T) {
	router := NewRouter(repository.NewMemStorage())
	request := newJSONRequest(
		t,
		http.MethodPost,
		"/value",
		`{"id":"UnknownGauge","type":"gauge"}`,
	)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusNotFound, response.Code)
	require.Equal(t, "application/json", response.Header().Get("Content-Type"))
}

func TestValueJSONRejectsMetricValueInLookup(t *testing.T) {
	router := NewRouter(repository.NewMemStorage())
	request := newJSONRequest(
		t,
		http.MethodPost,
		"/value",
		`{"id":"TestGauge","type":"gauge","value":1}`,
	)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Equal(t, "application/json", response.Header().Get("Content-Type"))
}

func TestUpdateJSONReturnsInternalErrorWhenSynchronousSaveFails(t *testing.T) {
	blockedParent := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(blockedParent, []byte("file"), 0o600))
	storage := repository.NewFileStorage(
		filepath.Join(blockedParent, "metrics-db.json"),
		true,
	)
	router := NewRouter(storage)
	request := newJSONRequest(
		t,
		http.MethodPost,
		"/update",
		`{"id":"TestGauge","type":"gauge","value":67.1}`,
	)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusInternalServerError, response.Code)
	require.JSONEq(t, `{"error":"failed to store metric"}`, response.Body.String())
}

func TestWriteJSONHandlesEncodingError(t *testing.T) {
	response := httptest.NewRecorder()

	writeJSON(response, http.StatusOK, make(chan int))

	require.Equal(t, http.StatusInternalServerError, response.Code)
	require.Equal(t, "application/json", response.Header().Get("Content-Type"))
	require.JSONEq(t, `{"error":"failed to encode response"}`, response.Body.String())
}

func newJSONRequest(t *testing.T, method string, target string, body string) *http.Request {
	t.Helper()

	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	return request
}

func decodeMetricResponse(t *testing.T, response *httptest.ResponseRecorder) models.Metrics {
	t.Helper()

	var metric models.Metrics
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &metric))
	return metric
}
