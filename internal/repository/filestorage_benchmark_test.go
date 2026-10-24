package repository

import (
	"fmt"
	"path/filepath"
	"testing"

	models "github.com/IvanSaratov/go-metrics-practice/internal/model"
)

func BenchmarkFileStorage(b *testing.B) {
	metrics := make([]models.Metrics, 0, 100)
	for i := range 50 {
		metrics = append(metrics,
			gaugeMetric(fmt.Sprintf("temperature_%d", i), 20+float64(i)/10),
			counterMetric(fmt.Sprintf("requests_%d", i), 1),
		)
	}

	for _, mode := range []struct {
		name        string
		synchronous bool
	}{
		{"Deferred", false},
		{"Synchronous", true},
	} {
		b.Run("UpdateBatch/"+mode.name, func(b *testing.B) {
			storage := NewFileStorage(
				filepath.Join(b.TempDir(), "metrics.json"),
				mode.synchronous,
			)
			if err := storage.UpdateBatch(testContext, metrics); err != nil {
				b.Fatal(err)
			}

			b.ReportAllocs()
			for b.Loop() {
				if err := storage.UpdateBatch(testContext, metrics); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
