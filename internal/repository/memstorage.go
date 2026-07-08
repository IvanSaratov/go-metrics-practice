package repository

type Storage interface {
	SetGauge(name string, value float64)
	AddCounter(name string, value int64)
	GetGauge(name string) (float64, bool)
	GetCounter(name string) (int64, bool)
	GetAllGauges() map[string]float64
	GetAllCounters() map[string]int64
}

type MemStorage struct {
	gauges   map[string]float64
	counters map[string]int64
}

func NewMemStorage() *MemStorage {
	return &MemStorage{
		gauges:   make(map[string]float64),
		counters: make(map[string]int64),
	}
}

func (m *MemStorage) SetGauge(name string, value float64) {
	m.gauges[name] = value
}

func (m *MemStorage) AddCounter(name string, value int64) {
	m.counters[name] += value
}

func (m *MemStorage) GetGauge(name string) (float64, bool) {
	value, ok := m.gauges[name]
	return value, ok
}

func (m *MemStorage) GetCounter(name string) (int64, bool) {
	value, ok := m.counters[name]
	return value, ok
}

func (m *MemStorage) GetAllGauges() map[string]float64 {
	gauges := make(map[string]float64, len(m.gauges))
	for name, value := range m.gauges {
		gauges[name] = value
	}

	return gauges
}

func (m *MemStorage) GetAllCounters() map[string]int64 {
	counters := make(map[string]int64, len(m.counters))
	for name, value := range m.counters {
		counters[name] = value
	}

	return counters
}
