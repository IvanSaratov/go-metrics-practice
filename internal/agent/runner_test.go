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

	"github.com/IvanSaratov/go-metrics-practice/internal/model"
	"github.com/stretchr/testify/require"
)

func TestAgentReportPreservesPendingCounters(t *testing.T) {
	for _, tt := range []struct {
		name        string
		failures    int
		wantPending int64
	}{
		{name: "successful report", wantPending: 3},
		{name: "failed report", failures: 1, wantPending: 13},
	} {
		t.Run(tt.name, func(t *testing.T) {
			metrics := NewMetrics()
			metrics.gauges["TestGauge"] = 67.1
			metrics.counters["TestCounter"] = 10
			sendErr := errors.New("send failed")
			httpClient := &flakyHTTPClient{
				failures: tt.failures, failureErr: sendErr,
				onRequest: func() {
					// За время отправки собраны новые значения.
					metrics.mu.Lock()
					defer metrics.mu.Unlock()
					metrics.counters["TestCounter"] += 3
					metrics.gauges["TestGauge"] = 99.0
				},
			}
			agent := NewAgent(metrics, NewClient("http://localhost", httpClient, "", nil), time.Hour, time.Hour, 1)

			err := agent.report(t.Context())

			if tt.failures == 0 {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, sendErr)
			}
			gauge, delta := 67.1, int64(10)
			require.Len(t, httpClient.received, 1)
			require.ElementsMatch(t, []model.Metrics{
				{ID: "TestGauge", MType: model.Gauge, Value: &gauge},
				{ID: "TestCounter", MType: model.Counter, Delta: &delta},
			}, httpClient.received[0])
			require.Equal(t, tt.wantPending, metrics.counters["TestCounter"])
			require.Equal(t, 99.0, metrics.gauges["TestGauge"])

			httpClient.onRequest = nil
			require.NoError(t, agent.report(t.Context()))

			gauge = 99.0
			require.Len(t, httpClient.received, 2)
			require.ElementsMatch(t, []model.Metrics{
				{ID: "TestGauge", MType: model.Gauge, Value: &gauge},
				{ID: "TestCounter", MType: model.Counter, Delta: &tt.wantPending},
			}, httpClient.received[1])
			require.Zero(t, metrics.counters["TestCounter"])
		})
	}
}

func TestAgentRunDoesNotRepeatCounters(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	metrics := NewMetrics()
	metrics.gauges["TestGauge"] = 67.1
	metrics.counters["TestCounter"] = 10
	httpClient := &flakyHTTPClient{}
	httpClient.onRequest = func() {
		if len(httpClient.received) >= 2 {
			cancel()
		}
	}
	agent := NewAgent(metrics, NewClient("http://localhost", httpClient, "", nil), time.Hour, time.Millisecond, 1)

	agent.Run(ctx)

	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.GreaterOrEqual(t, len(httpClient.received), 2)
	var total int64
	for _, batch := range httpClient.received {
		for _, metric := range batch {
			if metric.MType == model.Counter {
				require.NotNil(t, metric.Delta)
				total += *metric.Delta
			}
		}
	}
	require.Equal(t, int64(10), total)
	require.Zero(t, metrics.counters["TestCounter"])
}

func TestAgentRunPollsMetrics(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	metrics := NewMetrics()
	agent := NewAgent(
		metrics,
		NewClient("http://localhost", &recordingHTTPClient{}, "", nil),
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
	client := NewClient("http://localhost", httpClient, "", nil)
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
		NewClient("http://localhost", httpClient, "", nil),
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
		NewClient("http://localhost", httpClient, "", nil),
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
		NewClient("http://localhost", &recordingHTTPClient{}, "", nil),
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
		NewClient("http://localhost", &recordingHTTPClient{}, "", nil),
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
