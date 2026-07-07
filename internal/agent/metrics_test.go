package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPollRuntimeMetricsCollectsGaugeMetrics(t *testing.T) {
	metrics := NewMetrics()

	PollRuntimeMetrics(metrics)

	for _, name := range runtimeGaugeNames {
		_, ok := metrics.gauges[name]
		require.Truef(t, ok, "expected gauge %q to exist", name)
	}

	_, ok := metrics.gauges["RandomValue"]
	require.True(t, ok)
}

func TestPollRuntimeMetricsIncrementsPollCount(t *testing.T) {
	metrics := NewMetrics()

	PollRuntimeMetrics(metrics)
	PollRuntimeMetrics(metrics)

	require.Equal(t, int64(2), metrics.counters["PollCount"])
}

func TestReportMetricsSendsAllMetrics(t *testing.T) {
	metrics := NewMetrics()
	metrics.gauges["TestGauge"] = 67.1
	metrics.counters["TestCounter"] = 10
	sender := &fakeSender{
		gauges:   make(map[string]float64),
		counters: make(map[string]int64),
	}

	err := ReportMetrics(metrics, sender)

	require.NoError(t, err)
	require.Equal(t, 67.1, sender.gauges["TestGauge"])
	require.Equal(t, int64(10), sender.counters["TestCounter"])
}

type fakeSender struct {
	gauges   map[string]float64
	counters map[string]int64
}

func (f *fakeSender) SendGauge(name string, value float64) error {
	f.gauges[name] = value
	return nil
}

func (f *fakeSender) SendCounter(name string, value int64) error {
	f.counters[name] = value
	return nil
}
