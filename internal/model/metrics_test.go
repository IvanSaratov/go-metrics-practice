package models

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMetricsValidateUpdate(t *testing.T) {
	gaugeValue := 23.5
	counterDelta := int64(10)

	tests := []struct {
		name    string
		metric  Metrics
		wantErr bool
	}{
		{
			name:   "gauge",
			metric: Metrics{ID: "temperature", MType: Gauge, Value: &gaugeValue},
		},
		{
			name:   "counter",
			metric: Metrics{ID: "requests", MType: Counter, Delta: &counterDelta},
		},
		{
			name:    "missing id",
			metric:  Metrics{MType: Gauge, Value: &gaugeValue},
			wantErr: true,
		},
		{
			name:    "gauge without value",
			metric:  Metrics{ID: "temperature", MType: Gauge},
			wantErr: true,
		},
		{
			name: "gauge with delta",
			metric: Metrics{
				ID: "temperature", MType: Gauge,
				Value: &gaugeValue, Delta: &counterDelta,
			},
			wantErr: true,
		},
		{
			name:    "counter without delta",
			metric:  Metrics{ID: "requests", MType: Counter},
			wantErr: true,
		},
		{
			name: "counter with value",
			metric: Metrics{
				ID: "requests", MType: Counter,
				Delta: &counterDelta, Value: &gaugeValue,
			},
			wantErr: true,
		},
		{
			name:    "unsupported type",
			metric:  Metrics{ID: "metric", MType: "unknown"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.metric.ValidateUpdate()

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestValidateUpdatesChecksEveryMetric(t *testing.T) {
	gaugeValue := 23.5

	err := ValidateUpdates([]Metrics{
		{ID: "temperature", MType: Gauge, Value: &gaugeValue},
		{ID: "broken", MType: Counter},
	})

	require.Error(t, err)
}

func TestValidateUpdatesAcceptsEmptyBatch(t *testing.T) {
	require.NoError(t, ValidateUpdates(nil))
}
