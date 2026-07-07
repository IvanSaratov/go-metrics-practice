package repository

import (
	"testing"

	"github.com/stretchr/testify/require"
)

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

			storage.SetGauge(tt.metricName, tt.metricValue)

			value, ok := storage.GetGauge(tt.metricName)
			require.True(t, ok)
			require.Equal(t, tt.metricValue, value)
		})
	}
}

func TestMemStorageSetGaugeReplace(t *testing.T) {
	storage := NewMemStorage()

	storage.SetGauge("TestGauge", 67.0)
	storage.SetGauge("TestGauge", 76.1)

	value, ok := storage.GetGauge("TestGauge")
	require.True(t, ok)
	require.Equal(t, float64(76.1), value)
}

func TestMemStorageGetGaugeNotFound(t *testing.T) {
	storage := NewMemStorage()

	_, ok := storage.GetGauge("UnknownGauge")
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

			storage.AddCounter(tt.metricName, tt.metricValue)

			value, ok := storage.GetCounter(tt.metricName)
			require.True(t, ok)
			require.Equal(t, tt.metricValue, value)
		})
	}
}

func TestMemStorageAddCounterAddsValue(t *testing.T) {
	storage := NewMemStorage()

	storage.AddCounter("TestCounter", 67)
	storage.AddCounter("TestCounter", 10)

	value, ok := storage.GetCounter("TestCounter")
	require.True(t, ok)
	require.Equal(t, int64(77), value)
}

func TestMemStorageGetCounterNotFound(t *testing.T) {
	storage := NewMemStorage()

	_, ok := storage.GetCounter("UnknownCounter")
	require.False(t, ok)
}
