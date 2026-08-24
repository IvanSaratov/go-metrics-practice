package agent

import (
	"testing"

	models "github.com/IvanSaratov/go-metrics-practice/internal/model"
	"github.com/stretchr/testify/require"
)

func TestMetricsCollectRuntimeMetricsCollectsGauges(t *testing.T) {
	metrics := NewMetrics()

	metrics.collectRuntimeMetrics()

	for _, name := range runtimeGaugeNames {
		_, ok := metrics.gauges[name]
		require.Truef(t, ok, "expected gauge %q to exist", name)
	}

	_, ok := metrics.gauges["RandomValue"]
	require.True(t, ok)
}

func TestMetricsCollectRuntimeMetricsIncrementsPollCount(t *testing.T) {
	metrics := NewMetrics()

	metrics.collectRuntimeMetrics()
	metrics.collectRuntimeMetrics()

	require.Equal(t, int64(2), metrics.counters["PollCount"])
}

func TestMetricsUpdateSystemMetrics(t *testing.T) {
	metrics := NewMetrics()

	metrics.updateSystemMetrics(systemMetrics{
		totalMemory:    4096,
		freeMemory:     1024,
		cpuUtilization: []float64{12.5, 98.25},
	})

	require.Equal(t, float64(4096), metrics.gauges["TotalMemory"])
	require.Equal(t, float64(1024), metrics.gauges["FreeMemory"])
	require.Equal(t, 12.5, metrics.gauges["CPUutilization1"])
	require.Equal(t, 98.25, metrics.gauges["CPUutilization2"])
	require.NotContains(t, metrics.gauges, "CPUutilization0")
}

func TestMetricsSnapshotBuildsBatch(t *testing.T) {
	metrics := NewMetrics()
	metrics.gauges["TestGauge"] = 67.1
	metrics.counters["TestCounter"] = 10

	snapshot := metrics.snapshot()

	gaugeValue := 67.1
	counterDelta := int64(10)
	require.ElementsMatch(t, []models.Metrics{
		{ID: "TestGauge", MType: models.Gauge, Value: &gaugeValue},
		{ID: "TestCounter", MType: models.Counter, Delta: &counterDelta},
	}, snapshot)
}

func TestMetricsSnapshotCopiesValues(t *testing.T) {
	metrics := NewMetrics()
	metrics.gauges["TestGauge"] = 67.1
	metrics.counters["TestCounter"] = 10

	snapshot := metrics.snapshot()
	for _, metric := range snapshot {
		if metric.Value != nil {
			*metric.Value = 100.1
		}
		if metric.Delta != nil {
			*metric.Delta = 20
		}
	}

	require.Equal(t, 67.1, metrics.gauges["TestGauge"])
	require.Equal(t, int64(10), metrics.counters["TestCounter"])
}
