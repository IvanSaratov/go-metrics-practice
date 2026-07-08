package agent

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAgentPollUpdatesMetrics(t *testing.T) {
	metrics := NewMetrics()
	agent := NewAgent(metrics, &fakeSender{}, time.Second, time.Second)

	agent.poll()

	require.Equal(t, int64(1), metrics.counters["PollCount"])
}

func TestAgentReportSendsSnapshot(t *testing.T) {
	metrics := NewMetrics()
	metrics.gauges["TestGauge"] = 67.1
	metrics.counters["TestCounter"] = 10
	sender := &fakeSender{
		gauges:   make(map[string]float64),
		counters: make(map[string]int64),
	}
	agent := NewAgent(metrics, sender, time.Second, time.Second)

	err := agent.report()

	require.NoError(t, err)
	require.Equal(t, 67.1, sender.gauges["TestGauge"])
	require.Equal(t, int64(10), sender.counters["TestCounter"])
}

func TestAgentRunStopsWhenContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	agent := NewAgent(NewMetrics(), &fakeSender{}, time.Second, time.Second)
	done := make(chan struct{})

	go func() {
		defer close(done)
		agent.Run(ctx)
	}()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected agent to stop after context cancellation")
	}
}
