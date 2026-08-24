package agent

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAgentRunPollsMetrics(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	metrics := NewMetrics()
	agent := NewAgent(
		metrics,
		NewClient("http://localhost", &recordingHTTPClient{}, ""),
		time.Millisecond,
		time.Hour,
		1,
	)
	agent.systemCollector = staticSystemMetricsCollector{}
	done := make(chan struct{})

	go func() {
		defer close(done)
		agent.Run(ctx)
	}()

	require.Eventually(t, func() bool {
		metrics.mu.RLock()
		defer metrics.mu.RUnlock()
		return metrics.counters["PollCount"] > 0
	}, 100*time.Millisecond, time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected agent to stop after context cancellation")
	}
}

func TestAgentRunContinuesWhenReportFails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	metrics := NewMetrics()
	metrics.gauges["TestGauge"] = 67.1
	httpClient := &recordingHTTPClient{
		err: errors.New("send batch failed"),
	}
	client := NewClient("http://localhost", httpClient, "")
	client.retry = retryWithoutDelay
	agent := NewAgent(
		metrics,
		client,
		time.Hour,
		time.Millisecond,
		1,
	)
	done := make(chan struct{})

	go func() {
		defer close(done)
		agent.Run(ctx)
	}()

	require.Eventually(t, func() bool {
		return httpClient.requests.Load() >= 5
	}, time.Second, time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected agent to stop after context cancellation")
	}
}

func TestAgentRunPollsMetricsWhileReportIsBlocked(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	metrics := NewMetrics()
	httpClient := &blockingHTTPClient{}
	agent := NewAgent(
		metrics,
		NewClient("http://localhost", httpClient, ""),
		time.Millisecond,
		time.Millisecond,
		1,
	)
	agent.systemCollector = staticSystemMetricsCollector{}
	done := make(chan struct{})

	go func() {
		defer close(done)
		agent.Run(ctx)
	}()

	require.Eventually(t, func() bool {
		return httpClient.current.Load() == 1
	}, time.Second, time.Millisecond)
	before := pollCount(metrics)
	require.Eventually(t, func() bool {
		return pollCount(metrics) > before
	}, time.Second, time.Millisecond)

	cancel()
	waitAgentStopped(t, done)
}

func TestAgentRunLimitsConcurrentReports(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	metrics := NewMetrics()
	metrics.gauges["TestGauge"] = 67.1
	httpClient := &blockingHTTPClient{}
	agent := NewAgent(
		metrics,
		NewClient("http://localhost", httpClient, ""),
		time.Hour,
		time.Millisecond,
		2,
	)
	done := make(chan struct{})

	go func() {
		defer close(done)
		agent.Run(ctx)
	}()

	require.Eventually(t, func() bool {
		return httpClient.current.Load() == 2
	}, time.Second, time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	require.Equal(t, int32(2), httpClient.maximum.Load())
	require.Equal(t, int32(2), httpClient.requests.Load())

	cancel()
	waitAgentStopped(t, done)
}

func TestAgentRunCollectsSystemMetrics(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	metrics := NewMetrics()
	agent := NewAgent(
		metrics,
		NewClient("http://localhost", &recordingHTTPClient{}, ""),
		time.Millisecond,
		time.Hour,
		1,
	)
	agent.systemCollector = staticSystemMetricsCollector{
		values: systemMetrics{
			totalMemory:    4096,
			freeMemory:     1024,
			cpuUtilization: []float64{12.5, 98.25},
		},
	}
	done := make(chan struct{})

	go func() {
		defer close(done)
		agent.Run(ctx)
	}()

	require.Eventually(t, func() bool {
		metrics.mu.RLock()
		defer metrics.mu.RUnlock()
		return metrics.gauges["TotalMemory"] == 4096 &&
			metrics.gauges["FreeMemory"] == 1024 &&
			metrics.gauges["CPUutilization1"] == 12.5 &&
			metrics.gauges["CPUutilization2"] == 98.25
	}, time.Second, time.Millisecond)

	cancel()
	waitAgentStopped(t, done)
}

func TestAgentRunPollsRuntimeWhileSystemCollectionIsBlocked(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	metrics := NewMetrics()
	systemCollector := &blockingSystemMetricsCollector{
		started: make(chan struct{}),
	}
	agent := NewAgent(
		metrics,
		NewClient("http://localhost", &recordingHTTPClient{}, ""),
		time.Millisecond,
		time.Hour,
		1,
	)
	agent.systemCollector = systemCollector
	done := make(chan struct{})

	go func() {
		defer close(done)
		agent.Run(ctx)
	}()

	select {
	case <-systemCollector.started:
	case <-time.After(time.Second):
		t.Fatal("expected system metrics collection to start")
	}
	before := pollCount(metrics)
	require.Eventually(t, func() bool {
		return pollCount(metrics) > before
	}, time.Second, time.Millisecond)

	cancel()
	waitAgentStopped(t, done)
}

func pollCount(metrics *Metrics) int64 {
	metrics.mu.RLock()
	defer metrics.mu.RUnlock()
	return metrics.counters["PollCount"]
}

func waitAgentStopped(t *testing.T, done <-chan struct{}) {
	t.Helper()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected agent to stop after context cancellation")
	}
}

type recordingHTTPClient struct {
	requests atomic.Int32
	err      error
}

func (c *recordingHTTPClient) Do(request *http.Request) (*http.Response, error) {
	c.requests.Add(1)
	if c.err != nil {
		return nil, c.err
	}

	_, _ = io.Copy(io.Discard, request.Body)
	_ = request.Body.Close()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{jsonContentType},
		},
		Body: io.NopCloser(strings.NewReader("[]")),
	}, nil
}

type blockingHTTPClient struct {
	requests atomic.Int32
	current  atomic.Int32
	maximum  atomic.Int32
}

func (c *blockingHTTPClient) Do(request *http.Request) (*http.Response, error) {
	_, _ = io.Copy(io.Discard, request.Body)
	_ = request.Body.Close()

	c.requests.Add(1)
	current := c.current.Add(1)
	defer c.current.Add(-1)
	for {
		maximum := c.maximum.Load()
		if current <= maximum || c.maximum.CompareAndSwap(maximum, current) {
			break
		}
	}

	<-request.Context().Done()
	return nil, request.Context().Err()
}

type staticSystemMetricsCollector struct {
	values systemMetrics
}

func (c staticSystemMetricsCollector) collect(context.Context) (systemMetrics, error) {
	return c.values, nil
}

type blockingSystemMetricsCollector struct {
	started     chan struct{}
	startedOnce sync.Once
}

func (c *blockingSystemMetricsCollector) collect(ctx context.Context) (systemMetrics, error) {
	c.startedOnce.Do(func() {
		close(c.started)
	})
	<-ctx.Done()
	return systemMetrics{}, ctx.Err()
}
