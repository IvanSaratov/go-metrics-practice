package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPollRuntimeMetricsCollectsGaugeMetrics(t *testing.T) {
	metrics := NewMetrics()

	PollRuntimeMetrics(metrics)

	for _, name := range runtimeGaugeNames {
		_, ok := metrics.Gauges[name]
		require.Truef(t, ok, "expected gauge %q to exist", name)
	}

	_, ok := metrics.Gauges["RandomValue"]
	require.True(t, ok)
}

func TestPollRuntimeMetricsIncrementsPollCount(t *testing.T) {
	metrics := NewMetrics()

	PollRuntimeMetrics(metrics)
	PollRuntimeMetrics(metrics)

	require.Equal(t, int64(2), metrics.Counters["PollCount"])
}
