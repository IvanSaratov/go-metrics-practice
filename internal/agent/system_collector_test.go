package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/mem"
	"github.com/stretchr/testify/require"
)

func TestGopsutilCollectorCollectsMemoryAndCPU(t *testing.T) {
	collector := gopsutilCollector{
		virtualMemory: func(context.Context) (*mem.VirtualMemoryStat, error) {
			return &mem.VirtualMemoryStat{Total: 4096, Free: 1024}, nil
		},
		cpuPercent: func(_ context.Context, interval time.Duration, perCPU bool) ([]float64, error) {
			require.Zero(t, interval)
			require.True(t, perCPU)
			return []float64{12.5, 98.25}, nil
		},
	}

	values, err := collector.collect(context.Background())

	require.NoError(t, err)
	require.Equal(t, systemMetrics{
		totalMemory:    4096,
		freeMemory:     1024,
		cpuUtilization: []float64{12.5, 98.25},
	}, values)
}

func TestGopsutilCollectorReturnsMemoryError(t *testing.T) {
	wantErr := errors.New("memory unavailable")
	collector := gopsutilCollector{
		virtualMemory: func(context.Context) (*mem.VirtualMemoryStat, error) {
			return nil, wantErr
		},
	}

	_, err := collector.collect(context.Background())

	require.ErrorIs(t, err, wantErr)
}

func TestGopsutilCollectorReturnsCPUError(t *testing.T) {
	wantErr := errors.New("cpu unavailable")
	collector := gopsutilCollector{
		virtualMemory: func(context.Context) (*mem.VirtualMemoryStat, error) {
			return &mem.VirtualMemoryStat{Total: 4096, Free: 1024}, nil
		},
		cpuPercent: func(context.Context, time.Duration, bool) ([]float64, error) {
			return nil, wantErr
		},
	}

	_, err := collector.collect(context.Background())

	require.ErrorIs(t, err, wantErr)
}
