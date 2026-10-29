// Package repository содержит хранилища метрик: в памяти, файле и PostgreSQL.
package repository

import (
	"context"

	models "github.com/IvanSaratov/go-metrics-practice/internal/model"
)

// Storage — общий интерфейс для хранилищ метрик.
type Storage interface {
	// SetGauge записывает новое значение gauge.
	SetGauge(ctx context.Context, name string, value float64) error
	// AddCounter прибавляет value к счётчику и возвращает новое значение.
	// Если счётчика ещё нет, отсчёт начинается с нуля.
	AddCounter(ctx context.Context, name string, value int64) (int64, error)
	// UpdateBatch сначала проверяет весь пакет, затем обновляет метрики.
	// Пустой пакет ничего не меняет.
	UpdateBatch(ctx context.Context, metrics []models.Metrics) error
	// GetGauge возвращает значение gauge и сообщает, найдена ли метрика.
	// Если метрики нет, ошибки не будет.
	GetGauge(ctx context.Context, name string) (float64, bool, error)
	// GetCounter возвращает значение counter и сообщает, найден ли счётчик.
	// Если счётчика нет, ошибки не будет.
	GetCounter(ctx context.Context, name string) (int64, bool, error)
	// GetAllGauges возвращает все gauge в новой map. Её можно менять.
	GetAllGauges(ctx context.Context) (map[string]float64, error)
	// GetAllCounters возвращает все counter в новой map. Её можно менять.
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
