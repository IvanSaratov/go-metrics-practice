package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	models "github.com/IvanSaratov/go-metrics-practice/internal/model"
	"github.com/IvanSaratov/go-metrics-practice/internal/repository"
)

func BenchmarkServer(b *testing.B) {
	metrics := make([]models.Metrics, 0, 100)
	for i := range 50 {
		value, delta := 20+float64(i)/10, int64(1)
		metrics = append(metrics,
			models.Metrics{ID: fmt.Sprintf("temperature_%d", i), MType: models.Gauge, Value: &value},
			models.Metrics{ID: fmt.Sprintf("requests_%d", i), MType: models.Counter, Delta: &delta},
		)
	}
	body, err := json.Marshal(metrics)
	if err != nil {
		b.Fatal(err)
	}

	for _, scenario := range []struct {
		name, method, path string
		body               []byte
	}{
		{"UpdateBatch", http.MethodPost, "/updates", body},
		{"GetMetric", http.MethodPost, "/value", []byte(`{"id":"temperature_0","type":"gauge"}`)},
		{"List", http.MethodGet, "/", nil},
	} {
		b.Run(scenario.name, func(b *testing.B) {
			storage := repository.NewMemStorage()
			if err := storage.UpdateBatch(context.Background(), metrics); err != nil {
				b.Fatal(err)
			}
			server := NewServer(storage, nil, nil)
			b.ReportAllocs()

			// Создание запроса и буфера ответа httptest тоже входит в замер.
			for b.Loop() {
				request := httptest.NewRequest(scenario.method, scenario.path, bytes.NewReader(scenario.body))
				request.Header.Set("Content-Type", "application/json")
				response := httptest.NewRecorder()
				server.ServeHTTP(response, request)
				if response.Code != http.StatusOK {
					b.Fatalf("status=%d, body=%s", response.Code, response.Body.String())
				}
			}
		})
	}
}
