package agent

import (
	"sort"
	"testing"
)

func BenchmarkAgent(b *testing.B) {
	metrics := NewMetrics()
	for i, name := range runtimeGaugeNames {
		metrics.gauges[name] = float64(i + 1)
	}
	metrics.gauges["RandomValue"] = 0.5
	metrics.counters["PollCount"] = 100
	metrics.updateSystemMetrics(systemMetrics{
		totalMemory:    16 << 30,
		freeMemory:     8 << 30,
		cpuUtilization: []float64{12.5, 25, 50, 75},
	})

	b.Run("Snapshot", func(b *testing.B) {
		count := len(metrics.gauges) + len(metrics.counters)
		b.ReportAllocs()
		for b.Loop() {
			if snapshot := metrics.snapshot(); len(snapshot) != count {
				b.Fatalf("snapshot: count=%d, want=%d", len(snapshot), count)
			}
		}
	})

	b.Run("EncodeGzipJSON", func(b *testing.B) {
		batch := metrics.snapshot()
		// Фиксируем порядок метрик, чтобы сжимать одинаковые данные при каждом запуске.
		sort.Slice(batch, func(i, j int) bool { return batch[i].ID < batch[j].ID })
		b.ReportAllocs()
		for b.Loop() {
			if _, err := encodeGzipJSON(batch); err != nil {
				b.Fatal(err)
			}
		}
	})
}
