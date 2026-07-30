package repository

import "sync"

type Storage interface {
	SetGauge(name string, value float64) error
	AddCounter(name string, value int64) (int64, error)
	GetGauge(name string) (float64, bool)
	GetCounter(name string) (int64, bool)
	GetAllGauges() map[string]float64
	GetAllCounters() map[string]int64
}

type metricsSnapshot struct {
	gauges   map[string]float64
	counters map[string]int64
}

type MemStorage struct {
	// Один мьютекс сохраняет согласованность обеих коллекций метрик
	mu       sync.RWMutex
	gauges   map[string]float64
	counters map[string]int64
}

func NewMemStorage() *MemStorage {
	return &MemStorage{
		gauges:   make(map[string]float64),
		counters: make(map[string]int64),
	}
}

func (m *MemStorage) SetGauge(name string, value float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.gauges[name] = value
	return nil
}

func (m *MemStorage) AddCounter(name string, value int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.counters[name] += value
	return m.counters[name], nil
}

func (m *MemStorage) GetGauge(name string) (float64, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	value, ok := m.gauges[name]
	return value, ok
}

func (m *MemStorage) GetCounter(name string) (int64, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	value, ok := m.counters[name]
	return value, ok
}

func (m *MemStorage) GetAllGauges() map[string]float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return cloneMap(m.gauges)
}

func (m *MemStorage) GetAllCounters() map[string]int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return cloneMap(m.counters)
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
