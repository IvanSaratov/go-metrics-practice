package repository

import (
	"context"
	"sync"

	models "github.com/IvanSaratov/go-metrics-practice/internal/model"
)

type metricsSnapshot struct {
	gauges   map[string]float64
	counters map[string]int64
}

// MemStorage хранит метрики в памяти. Можно использовать из нескольких горутин.
// Контекст в методах не используется.
type MemStorage struct {
	// Один мьютекс сохраняет согласованность обеих коллекций метрик
	mu       sync.RWMutex
	gauges   map[string]float64
	counters map[string]int64
}

// NewMemStorage создаёт пустое хранилище метрик в памяти.
func NewMemStorage() *MemStorage {
	return &MemStorage{
		gauges:   make(map[string]float64),
		counters: make(map[string]int64),
	}
}

// SetGauge записывает новое значение gauge.
func (m *MemStorage) SetGauge(_ context.Context, name string, value float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.gauges[name] = value
	return nil
}

// AddCounter прибавляет value к счётчику и возвращает новое значение.
func (m *MemStorage) AddCounter(_ context.Context, name string, value int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.counters[name] += value
	return m.counters[name], nil
}

// UpdateBatch проверяет пакет и обновляет все метрики под одной блокировкой.
// Если в пакете есть ошибка, данные не меняются.
func (m *MemStorage) UpdateBatch(_ context.Context, metrics []models.Metrics) error {
	// Общеинтерфейсный метод
	if err := models.ValidateUpdates(metrics); err != nil {
		return err
	}
	if len(metrics) == 0 {
		return nil
	}

	// Будем блокировать только один раз для memory
	m.mu.Lock()
	defer m.mu.Unlock()

	applyBatch(metricsSnapshot{
		gauges:   m.gauges,
		counters: m.counters,
	}, metrics)

	return nil
}

// GetGauge возвращает значение gauge и сообщает, найдена ли метрика.
func (m *MemStorage) GetGauge(_ context.Context, name string) (float64, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	value, ok := m.gauges[name]
	return value, ok, nil
}

// GetCounter возвращает значение counter и сообщает, найден ли счётчик.
func (m *MemStorage) GetCounter(_ context.Context, name string) (int64, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	value, ok := m.counters[name]
	return value, ok, nil
}

// GetAllGauges возвращает копию map с gauge-метриками.
func (m *MemStorage) GetAllGauges(_ context.Context) (map[string]float64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return cloneMap(m.gauges), nil
}

// GetAllCounters возвращает копию map со счётчиками.
func (m *MemStorage) GetAllCounters(_ context.Context) (map[string]int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return cloneMap(m.counters), nil
}

func (m *MemStorage) snapshot() metricsSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return metricsSnapshot{
		gauges:   cloneMap(m.gauges),
		counters: cloneMap(m.counters),
	}
}

func (m *MemStorage) replace(snapshot metricsSnapshot) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.gauges = cloneMap(snapshot.gauges)
	m.counters = cloneMap(snapshot.counters)
}

func cloneMap[K comparable, V any](source map[K]V) map[K]V {
	clone := make(map[K]V, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}
