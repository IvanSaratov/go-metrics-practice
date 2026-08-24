package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
)

type systemMetricsCollector interface {
	collect(context.Context) (systemMetrics, error)
}

type gopsutilCollector struct {
	virtualMemory func(context.Context) (*mem.VirtualMemoryStat, error)
	cpuPercent    func(context.Context, time.Duration, bool) ([]float64, error)
}

var defaultSystemMetricsCollector = gopsutilCollector{
	virtualMemory: mem.VirtualMemoryWithContext,
	cpuPercent:    cpu.PercentWithContext,
}

// получает сведения о памяти и загрузке каждого CPU
func (c gopsutilCollector) collect(ctx context.Context) (systemMetrics, error) {
	memory, err := c.virtualMemory(ctx)
	if err != nil {
		return systemMetrics{}, fmt.Errorf("collect memory metrics: %w", err)
	}

	cpuUtilization, err := c.cpuPercent(ctx, 0, true)
	if err != nil {
		return systemMetrics{}, fmt.Errorf("collect CPU metrics: %w", err)
	}

	return systemMetrics{
		totalMemory:    memory.Total,
		freeMemory:     memory.Free,
		cpuUtilization: cpuUtilization,
	}, nil
}
