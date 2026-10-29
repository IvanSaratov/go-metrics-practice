package repository

import (
	"fmt"
	"testing"

	models "github.com/IvanSaratov/go-metrics-practice/internal/model"
)

func BenchmarkMemStorage(b *testing.B) {
	metrics := make([]models.Metrics, 0, 100)
	for i := range 50 {
		metrics = append(metrics,
			gaugeMetric(fmt.Sprintf("temperature_%d", i), 20+float64(i)/10),
			counterMetric(fmt.Sprintf("requests_%d", i), 1),
		)
	}

	storage := NewMemStorage()
	if err := storage.UpdateBatch(testContext, metrics); err != nil {
		b.Fatal(err)
	}

	b.Run("UpdateBatch", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if err := storage.UpdateBatch(testContext, metrics); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("GetGauge", func(b *testing.B) {
		name := metrics[0].ID
		b.ReportAllocs()
		for b.Loop() {
			_, found, err := storage.GetGauge(testContext, name)
			if err != nil || !found {
				b.Fatalf("get gauge: found=%t, err=%v", found, err)
			}
		}
	})

	b.Run("GetAllGauges", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			gauges, err := storage.GetAllGauges(testContext)
			if err != nil || len(gauges) != 50 {
				b.Fatalf("get all gauges: count=%d, err=%v", len(gauges), err)
			}
		}
	})
}
