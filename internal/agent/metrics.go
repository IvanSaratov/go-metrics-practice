package agent

import (
	"fmt"
	"math/rand"
	"runtime"
	"sync"

	"github.com/IvanSaratov/go-metrics-practice/internal/model"
)

// Metrics хранит собранные метрики. Можно использовать из нескольких горутин.
type Metrics struct {
	mu       sync.RWMutex
	gauges   map[string]float64
	counters map[string]int64
}

type systemMetrics struct {
	totalMemory    uint64
	freeMemory     uint64
	cpuUtilization []float64
}

var runtimeGaugeNames = []string{
	"Alloc",
	"BuckHashSys",
	"Frees",
	"GCCPUFraction",
	"GCSys",
	"HeapAlloc",
	"HeapIdle",
	"HeapInuse",
	"HeapObjects",
	"HeapReleased",
	"HeapSys",
	"LastGC",
	"Lookups",
	"MCacheInuse",
	"MCacheSys",
	"MSpanInuse",
	"MSpanSys",
	"Mallocs",
	"NextGC",
	"NumForcedGC",
	"NumGC",
	"OtherSys",
	"PauseTotalNs",
	"StackInuse",
	"StackSys",
	"Sys",
	"TotalAlloc",
}

// NewMetrics создаёт пустое хранилище метрик агента.
func NewMetrics() *Metrics {
	return &Metrics{
		gauges:   make(map[string]float64),
		counters: make(map[string]int64),
	}
}

func (m *Metrics) collectRuntimeMetrics() {
	m.mu.Lock()
	defer m.mu.Unlock()

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	runtimeGauges := map[string]float64{
		"Alloc":         float64(memStats.Alloc),
		"BuckHashSys":   float64(memStats.BuckHashSys),
		"Frees":         float64(memStats.Frees),
		"GCCPUFraction": memStats.GCCPUFraction,
		"GCSys":         float64(memStats.GCSys),
		"HeapAlloc":     float64(memStats.HeapAlloc),
		"HeapIdle":      float64(memStats.HeapIdle),
		"HeapInuse":     float64(memStats.HeapInuse),
		"HeapObjects":   float64(memStats.HeapObjects),
		"HeapReleased":  float64(memStats.HeapReleased),
		"HeapSys":       float64(memStats.HeapSys),
		"LastGC":        float64(memStats.LastGC),
		"Lookups":       float64(memStats.Lookups),
		"MCacheInuse":   float64(memStats.MCacheInuse),
		"MCacheSys":     float64(memStats.MCacheSys),
		"MSpanInuse":    float64(memStats.MSpanInuse),
		"MSpanSys":      float64(memStats.MSpanSys),
		"Mallocs":       float64(memStats.Mallocs),
		"NextGC":        float64(memStats.NextGC),
		"NumForcedGC":   float64(memStats.NumForcedGC),
		"NumGC":         float64(memStats.NumGC),
		"OtherSys":      float64(memStats.OtherSys),
		"PauseTotalNs":  float64(memStats.PauseTotalNs),
		"StackInuse":    float64(memStats.StackInuse),
		"StackSys":      float64(memStats.StackSys),
		"Sys":           float64(memStats.Sys),
		"TotalAlloc":    float64(memStats.TotalAlloc),
	}

	for _, name := range runtimeGaugeNames {
		m.gauges[name] = runtimeGauges[name]
	}

	// требуется по заданию инкремента
	m.gauges["RandomValue"] = rand.Float64()

	m.counters["PollCount"]++
}

// сохраняет память и загрузку каждого CPU как gauge-метрики
func (m *Metrics) updateSystemMetrics(values systemMetrics) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.gauges["TotalMemory"] = float64(values.totalMemory)
	m.gauges["FreeMemory"] = float64(values.freeMemory)
	for index, utilization := range values.cpuUtilization {
		m.gauges[fmt.Sprintf("CPUutilization%d", index+1)] = utilization
	}
}

func (m *Metrics) snapshot() []model.Metrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.snapshotLocked()
}

// Забирает счётчики для одного пакета, чтобы другой воркер не отправил их повторно.
func (m *Metrics) takeSnapshot() []model.Metrics {
	m.mu.Lock()
	defer m.mu.Unlock()

	metrics := m.snapshotLocked()
	clear(m.counters)
	return metrics
}

// Возвращает неотправленные прибавки, сохраняя новые счётчики и актуальные gauge.
func (m *Metrics) restoreCounters(metrics []model.Metrics) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, metric := range metrics {
		if metric.MType == model.Counter {
			m.counters[metric.ID] += *metric.Delta
		}
	}
}

// Вызывается только при уже взятой блокировке m.mu.
func (m *Metrics) snapshotLocked() []model.Metrics {
	metrics := make([]model.Metrics, 0, len(m.gauges)+len(m.counters))

	for name, value := range m.gauges {
		metrics = append(metrics, model.Metrics{
			ID:    name,
			MType: model.Gauge,
			Value: &value,
		})
	}

	for name, value := range m.counters {
		metrics = append(metrics, model.Metrics{
			ID:    name,
			MType: model.Counter,
			Delta: &value,
		})
	}

	return metrics
}
