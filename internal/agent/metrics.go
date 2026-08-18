package agent

import (
	"math/rand"
	"runtime"
	"sync"

	models "github.com/IvanSaratov/go-metrics-practice/internal/model"
)

type Metrics struct {
	mu       sync.RWMutex
	gauges   map[string]float64
	counters map[string]int64
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

func (m *Metrics) snapshot() []models.Metrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	metrics := make([]models.Metrics, 0, len(m.gauges)+len(m.counters))

	for name, value := range m.gauges {
		metrics = append(metrics, models.Metrics{
			ID:    name,
			MType: models.Gauge,
			Value: &value,
		})
	}

	for name, value := range m.counters {
		metrics = append(metrics, models.Metrics{
			ID:    name,
			MType: models.Counter,
			Delta: &value,
		})
	}

	return metrics
}
