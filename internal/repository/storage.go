package repository

import (
	"context"

	models "github.com/IvanSaratov/go-metrics-practice/internal/model"
)

type Storage interface {
	SetGauge(ctx context.Context, name string, value float64) error
	AddCounter(ctx context.Context, name string, value int64) (int64, error)
	UpdateBatch(ctx context.Context, metrics []models.Metrics) error
	GetGauge(ctx context.Context, name string) (float64, bool, error)
	GetCounter(ctx context.Context, name string) (int64, bool, error)
	GetAllGauges(ctx context.Context) (map[string]float64, error)
	GetAllCounters(ctx context.Context) (map[string]int64, error)
}

// Добавляем общую функцию интерфейса валидации батчей
func applyBatch(snapshot metricsSnapshot, metrics []models.Metrics) {
	for _, metric := range metrics {
		switch metric.MType {
		case models.Gauge:
			snapshot.gauges[metric.ID] = *metric.Value
		case models.Counter:
			snapshot.counters[metric.ID] += *metric.Delta
		}
	}
}
