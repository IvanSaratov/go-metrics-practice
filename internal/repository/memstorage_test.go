package repository

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

var testContext = context.Background()

func TestMemStorageSetGauge(t *testing.T) {
	tests := []struct {
		name        string
		metricName  string
		metricValue float64
	}{
		{
			name:        "positive value",
			metricName:  "TestGauge",
			metricValue: 67.1,
		},
		{
			name:        "negative value",
			metricName:  "TestGauge",
			metricValue: -67.1,
		},
		{
			name:        "zero value",
			metricName:  "TestGauge",
			metricValue: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := NewMemStorage()

			require.NoError(t, storage.SetGauge(testContext, tt.metricName, tt.metricValue))

			value, ok, err := storage.GetGauge(testContext, tt.metricName)
			require.NoError(t, err)
			require.True(t, ok)
			require.Equal(t, tt.metricValue, value)
		})
	}
}

func TestMemStorageSetGaugeReplace(t *testing.T) {
	storage := NewMemStorage()

	require.NoError(t, storage.SetGauge(testContext, "TestGauge", 67.0))
	require.NoError(t, storage.SetGauge(testContext, "TestGauge", 76.1))

	value, ok, err := storage.GetGauge(testContext, "TestGauge")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, float64(76.1), value)
}

func TestMemStorageGetGaugeNotFound(t *testing.T) {
	storage := NewMemStorage()

	_, ok, err := storage.GetGauge(testContext, "UnknownGauge")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestMemStorageAddCounter(t *testing.T) {
	tests := []struct {
		name        string
		metricName  string
		metricValue int64
	}{
		{
			name:        "positive value",
			metricName:  "TestCounter",
			metricValue: 67,
		},
		{
			name:        "negative value",
			metricName:  "TestCounter",
			metricValue: -67,
		},
		{
			name:        "zero value",
			metricName:  "TestCounter",
			metricValue: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := NewMemStorage()

			_, err := storage.AddCounter(testContext, tt.metricName, tt.metricValue)
			require.NoError(t, err)

			value, ok, err := storage.GetCounter(testContext, tt.metricName)
			require.NoError(t, err)
			require.True(t, ok)
			require.Equal(t, tt.metricValue, value)
		})
	}
}

func TestMemStorageAddCounterAddsValue(t *testing.T) {
	storage := NewMemStorage()

	_, err := storage.AddCounter(testContext, "TestCounter", 67)
	require.NoError(t, err)
	total, err := storage.AddCounter(testContext, "TestCounter", 10)

	require.NoError(t, err)
	value, ok, err := storage.GetCounter(testContext, "TestCounter")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, int64(77), value)
	require.Equal(t, int64(77), total)
}

func TestMemStorageGetCounterNotFound(t *testing.T) {
	storage := NewMemStorage()

	_, ok, err := storage.GetCounter(testContext, "UnknownCounter")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestMemStorageGetAllGauges(t *testing.T) {
	storage := NewMemStorage()
	require.NoError(t, storage.SetGauge(testContext, "FirstGauge", 67.1))
	require.NoError(t, storage.SetGauge(testContext, "SecondGauge", 76.1))

	gauges, err := storage.GetAllGauges(testContext)
	require.NoError(t, err)

	require.Equal(t, map[string]float64{
		"FirstGauge":  67.1,
		"SecondGauge": 76.1,
	}, gauges)
}

func TestMemStorageGetAllGaugesReturnsCopy(t *testing.T) {
	storage := NewMemStorage()
	require.NoError(t, storage.SetGauge(testContext, "TestGauge", 67.1))

	gauges, err := storage.GetAllGauges(testContext)
	require.NoError(t, err)
	gauges["TestGauge"] = 100.1

	value, ok, err := storage.GetGauge(testContext, "TestGauge")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 67.1, value)
}

func TestMemStorageGetAllCounters(t *testing.T) {
	storage := NewMemStorage()
	_, err := storage.AddCounter(testContext, "FirstCounter", 67)
	require.NoError(t, err)
	_, err = storage.AddCounter(testContext, "SecondCounter", 76)
	require.NoError(t, err)

	counters, err := storage.GetAllCounters(testContext)
	require.NoError(t, err)

	require.Equal(t, map[string]int64{
		"FirstCounter":  67,
		"SecondCounter": 76,
	}, counters)
}

func TestMemStorageGetAllCountersReturnsCopy(t *testing.T) {
	storage := NewMemStorage()
	_, err := storage.AddCounter(testContext, "TestCounter", 67)
	require.NoError(t, err)

	counters, err := storage.GetAllCounters(testContext)
	require.NoError(t, err)
	counters["TestCounter"] = 100

	value, ok, err := storage.GetCounter(testContext, "TestCounter")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, int64(67), value)
}

func TestMemStorageConcurrentAccess(t *testing.T) {
	const (
		workers    = 16
		iterations = 100
	)

	storage := NewMemStorage()
	var wg sync.WaitGroup
	wg.Add(workers)

	for range workers {
		go func() {
			defer wg.Done()

			for i := range iterations {
				_ = storage.SetGauge(testContext, "TestGauge", float64(i))
				_, _ = storage.AddCounter(testContext, "TestCounter", 1)
				_, _, _ = storage.GetGauge(testContext, "TestGauge")
				_, _, _ = storage.GetCounter(testContext, "TestCounter")
				_, _ = storage.GetAllGauges(testContext)
				_, _ = storage.GetAllCounters(testContext)
			}
		}()
	}

	wg.Wait()

	value, ok, err := storage.GetCounter(testContext, "TestCounter")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, int64(workers*iterations), value)
}
