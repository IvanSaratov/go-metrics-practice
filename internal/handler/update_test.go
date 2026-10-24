package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/IvanSaratov/go-metrics-practice/internal/audit"
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

	value, ok, err := storage.GetGauge(context.Background(), "TestGauge")
	require.NoError(t, err)
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

	value, ok, err := storage.GetCounter(context.Background(), "TestCounter")
	require.NoError(t, err)
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
			_, ok, err := storage.GetGauge(context.Background(), "TestGauge")
			require.NoError(t, err)
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

// Проверяем аудит каждого способа обновления и IP фактического соединения.
func TestUpdateHandlerAuditsSuccessfulUpdates(t *testing.T) {
	for _, tt := range []struct {
		name       string
		path       string
		body       string
		remoteAddr string
		wantIP     string
		wantNames  []string
	}{
		{
			name: "gauge path", path: "/update/gauge/Alloc/12.5",
			remoteAddr: "192.168.0.42:1234", wantIP: "192.168.0.42",
			wantNames: []string{"Alloc"},
		},
		{
			name: "counter path", path: "/update/counter/PollCount/1",
			remoteAddr: "[2001:db8::1]:1234", wantIP: "2001:db8::1",
			wantNames: []string{"PollCount"},
		},
		{
			name: "gauge JSON", path: "/update",
			body:       `{"id":"Alloc","type":"gauge","value":12.5}`,
			remoteAddr: "192.168.0.42", wantIP: "192.168.0.42",
			wantNames: []string{"Alloc"},
		},
		{
			name: "counter JSON", path: "/update",
			body:       `{"id":"PollCount","type":"counter","delta":1}`,
			remoteAddr: "2001:db8::1", wantIP: "2001:db8::1",
			wantNames: []string{"PollCount"},
		},
		{
			name: "batch", path: "/updates",
			body: `[
				{"id":"Alloc","type":"gauge","value":12.5},
				{"id":"Frees","type":"gauge","value":5},
				{"id":"Alloc","type":"gauge","value":14}
			]`,
			remoteAddr: "192.168.0.42:1234", wantIP: "192.168.0.42",
			wantNames: []string{"Alloc", "Frees", "Alloc"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			requestContext, cancel := context.WithCancel(context.Background())
			defer cancel()
			var events []audit.Event
			server := NewServer(&readErrorStorage{}, nil, func(ctx context.Context, event audit.Event) {
				require.Equal(t, requestContext.Done(), ctx.Done())
				events = append(events, event)
			})
			request := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			request = request.WithContext(requestContext)
			request.RemoteAddr = tt.remoteAddr
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-Forwarded-For", "203.0.113.1")
			response := httptest.NewRecorder()
			before := time.Now().Unix()

			server.ServeHTTP(response, request)

			require.Equal(t, http.StatusOK, response.Code)
			require.Len(t, events, 1)
			require.Equal(t, tt.wantNames, events[0].Metrics)
			require.Equal(t, tt.wantIP, events[0].IPAddress)
			require.GreaterOrEqual(t, events[0].Timestamp, before)
			require.LessOrEqual(t, events[0].Timestamp, time.Now().Unix())
		})
	}
}

// Ошибочные обновления, чтение и пустой пакет не должны создавать аудит.
func TestUpdateHandlerSkipsAuditWithoutSuccessfulMetrics(t *testing.T) {
	storeErr := errors.New("store failed")
	for _, tt := range []struct {
		name     string
		method   string
		path     string
		body     string
		storeErr error
		status   int
	}{
		{
			name: "invalid gauge path", method: http.MethodPost,
			path: "/update/gauge/Alloc/bad", status: http.StatusBadRequest,
		},
		{
			name: "invalid counter path", method: http.MethodPost,
			path: "/update/counter/PollCount/bad", status: http.StatusBadRequest,
		},
		{
			name: "invalid JSON", method: http.MethodPost,
			path: "/update", body: `{`, status: http.StatusBadRequest,
		},
		{
			name: "invalid batch", method: http.MethodPost, path: "/updates",
			body: `[{"id":"Alloc","type":"gauge"}]`, status: http.StatusBadRequest,
		},
		{
			name: "gauge storage failure", method: http.MethodPost,
			path: "/update/gauge/Alloc/1", storeErr: storeErr, status: http.StatusInternalServerError,
		},
		{
			name: "counter storage failure", method: http.MethodPost,
			path: "/update/counter/PollCount/1", storeErr: storeErr, status: http.StatusInternalServerError,
		},
		{
			name: "JSON storage failure", method: http.MethodPost, path: "/update",
			body: `{"id":"Alloc","type":"gauge","value":1}`, storeErr: storeErr, status: http.StatusInternalServerError,
		},
		{
			name: "batch storage failure", method: http.MethodPost, path: "/updates",
			body: `[{"id":"Alloc","type":"gauge","value":1}]`, storeErr: storeErr, status: http.StatusInternalServerError,
		},
		{
			name: "empty batch", method: http.MethodPost,
			path: "/updates", body: `[]`, status: http.StatusOK,
		},
		{
			name: "read request", method: http.MethodGet, path: "/", status: http.StatusOK,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			server := NewServer(&readErrorStorage{writeErr: tt.storeErr}, nil, func(context.Context, audit.Event) { calls++ })
			request := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()

			server.ServeHTTP(response, request)

			require.Equal(t, tt.status, response.Code)
			require.Zero(t, calls)
		})
	}
}
