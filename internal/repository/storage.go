package repository

import (
	"context"
	"fmt"

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
func validateBatch(metrics []models.Metrics) error {
	for _, metric := range metrics {
		if metric.ID == "" {
			return fmt.Errorf("metric id is required")
		}

		switch metric.MType {
		case models.Gauge:
			if metric.Value == nil || metric.Delta != nil {
				return fmt.Errorf("gauge %q requires value only", metric.ID)
			}
		case models.Counter:
			if metric.Delta == nil || metric.Value != nil {
				return fmt.Errorf("counter %q requires delta only", metric.ID)
			}
		default:
			return fmt.Errorf("unsupported metric type %q", metric.MType)
		}
	}

	return nil
}

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
