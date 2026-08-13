// Создаем отдельный проверочный unit тест для всех http проверок,
// так как тут намешаны тесты сразу из всех типов данных

package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

var errStorageRead = errors.New("storage read failed")

type readErrorStorage struct {
	gaugeErr       error
	counterErr     error
	allGaugesErr   error
	allCountersErr error
}

func (s *readErrorStorage) SetGauge(context.Context, string, float64) error {
	return nil
}

func (s *readErrorStorage) AddCounter(context.Context, string, int64) (int64, error) {
	return 0, nil
}

func (s *readErrorStorage) GetGauge(context.Context, string) (float64, bool, error) {
	return 0, false, s.gaugeErr
}

func (s *readErrorStorage) GetCounter(context.Context, string) (int64, bool, error) {
	return 0, false, s.counterErr
}

func (s *readErrorStorage) GetAllGauges(context.Context) (map[string]float64, error) {
	return map[string]float64{}, s.allGaugesErr
}

func (s *readErrorStorage) GetAllCounters(context.Context) (map[string]int64, error) {
	return map[string]int64{}, s.allCountersErr
}

func TestValueHandlerReturnsInternalServerErrorWhenStorageReadFails(t *testing.T) {
	tests := []struct {
		name    string
		storage *readErrorStorage
		request func(t *testing.T) *http.Request
	}{
		{
			name:    "gauge path",
			storage: &readErrorStorage{gaugeErr: errStorageRead},
			request: func(t *testing.T) *http.Request {
				return httptest.NewRequest(http.MethodGet, "/value/gauge/TestGauge", nil)
			},
		},
		{
			name:    "counter path",
			storage: &readErrorStorage{counterErr: errStorageRead},
			request: func(t *testing.T) *http.Request {
				return httptest.NewRequest(http.MethodGet, "/value/counter/TestCounter", nil)
			},
		},
		{
			name:    "gauge JSON",
			storage: &readErrorStorage{gaugeErr: errStorageRead},
			request: func(t *testing.T) *http.Request {
				return newJSONRequest(
					t,
					http.MethodPost,
					"/value",
					`{"id":"TestGauge","type":"gauge"}`,
				)
			},
		},
		{
			name:    "counter JSON",
			storage: &readErrorStorage{counterErr: errStorageRead},
			request: func(t *testing.T) *http.Request {
				return newJSONRequest(
					t,
					http.MethodPost,
					"/value",
					`{"id":"TestCounter","type":"counter"}`,
				)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newTestServer(tt.storage)
			response := httptest.NewRecorder()

			server.ServeHTTP(response, tt.request(t))

			require.Equal(t, http.StatusInternalServerError, response.Code)
		})
	}
}

func TestListHandlerReturnsInternalServerErrorWhenStorageReadFails(t *testing.T) {
	tests := []struct {
		name    string
		storage *readErrorStorage
	}{
		{
			name:    "gauges",
			storage: &readErrorStorage{allGaugesErr: errStorageRead},
		},
		{
			name:    "counters",
			storage: &readErrorStorage{allCountersErr: errStorageRead},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newTestServer(tt.storage)
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			response := httptest.NewRecorder()

			server.ServeHTTP(response, request)

			require.Equal(t, http.StatusInternalServerError, response.Code)
		})
	}
}
