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
	"testing/synctest"
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

	require.NoError(t, agent.Run(ctx))

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

func TestAgentRunFlushesPendingMetrics(t *testing.T) {
	sendErr := errors.New("final report failed")
	for _, tt := range []struct {
		name    string
		pending int64
		err     error
	}{
		{name: "empty metrics"},
		{name: "pending metrics", pending: 10},
		{name: "failed final report", pending: 10, err: sendErr},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			metrics := NewMetrics()
			if tt.pending != 0 {
				metrics.counters["TestCounter"] = tt.pending
			}
			httpClient := &recordingHTTPClient{err: tt.err}
			agent := NewAgent(metrics, NewClient("http://localhost", httpClient, "", nil), time.Hour, time.Hour, 1)

			err := agent.Run(ctx)

			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)
				require.Equal(t, tt.pending, metrics.counters["TestCounter"])
			} else {
				require.NoError(t, err)
				require.Zero(t, metrics.counters["TestCounter"])
			}
			if tt.pending == 0 {
				require.Zero(t, httpClient.requests.Load())
			} else {
				require.Equal(t, int32(1), httpClient.requests.Load())
			}
		})
	}
}

func TestAgentRunWaitsForInFlightReport(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		release := make(chan struct{})
		releaseRequest := sync.OnceFunc(func() { close(release) })
		defer releaseRequest()
		metrics := NewMetrics()
		metrics.gauges["TestGauge"] = 67.1
		metrics.counters["TestCounter"] = 10
		httpClient := &blockingHTTPClient{started: make(chan struct{}), release: release}
		agent := NewAgent(metrics, NewClient("http://localhost", httpClient, "", nil), time.Hour, time.Millisecond, 1)
		done := make(chan error, 1)
		go func() { done <- agent.Run(ctx) }()
		<-httpClient.started

		cancel()
		synctest.Wait()

		require.Equal(t, int32(1), httpClient.current.Load(), "signal interrupted the active request")
		select {
		case err := <-done:
			t.Fatalf("agent stopped before the request completed: %v", err)
		default:
		}

		releaseRequest()
		require.NoError(t, waitAgentStopped(t, done))
		require.Zero(t, httpClient.current.Load())
		require.Equal(t, int32(2), httpClient.requests.Load())
		require.Equal(t, int32(1), httpClient.maximum.Load(), "final report started before the worker stopped")
	})
}

func TestAgentRunWaitsForSystemCollection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		release := make(chan struct{})
		releaseCollection := sync.OnceFunc(func() { close(release) })
		defer releaseCollection()
		collector := &blockingSystemMetricsCollector{
			started: make(chan struct{}), release: release,
			values: systemMetrics{totalMemory: 4096},
		}
		httpClient := &flakyHTTPClient{}
		agent := NewAgent(NewMetrics(), NewClient("http://localhost", httpClient, "", nil), time.Millisecond, time.Hour, 1)
		agent.systemCollector = collector
		done := make(chan error, 1)
		go func() { done <- agent.Run(ctx) }()
		<-collector.started

		cancel()
		synctest.Wait()

		select {
		case err := <-done:
			t.Fatalf("agent stopped before system metrics were collected: %v", err)
		default:
		}
		require.Empty(t, httpClient.received, "final report started before collection finished")

		releaseCollection()
		require.NoError(t, waitAgentStopped(t, done))
		require.Equal(t, int32(1), collector.calls.Load())
		require.Len(t, httpClient.received, 1)
		totalMemory := 4096.0
		require.Contains(t, httpClient.received[0], model.Metrics{
			ID: "TotalMemory", MType: model.Gauge, Value: &totalMemory,
		})
	})
}

func TestAgentRunPollsMetrics(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	metrics := NewMetrics()
	httpClient := &flakyHTTPClient{}
	agent := NewAgent(
		metrics,
		NewClient("http://localhost", httpClient, "", nil),
		time.Millisecond,
		time.Hour,
		1,
	)
	agent.systemCollector = staticSystemMetricsCollector{}
	done := make(chan error, 1)

	go func() {
		done <- agent.Run(ctx)
	}()

	require.Eventually(t, func() bool {
		metrics.mu.RLock()
		defer metrics.mu.RUnlock()
		return metrics.counters["PollCount"] > 0
	}, 100*time.Millisecond, time.Millisecond)
	cancel()
	require.NoError(t, waitAgentStopped(t, done))
	require.Len(t, httpClient.received, 1)
	var reportedPollCount int64
	for _, metric := range httpClient.received[0] {
		if metric.ID == "PollCount" {
			require.NotNil(t, metric.Delta)
			reportedPollCount = *metric.Delta
		}
	}
	require.Positive(t, reportedPollCount)
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
	done := make(chan error, 1)

	go func() {
		done <- agent.Run(ctx)
	}()

	require.Eventually(t, func() bool {
		return httpClient.requests.Load() >= 5
	}, time.Second, time.Millisecond)
	cancel()

	require.ErrorIs(t, waitAgentStopped(t, done), httpClient.err)
}

func TestAgentRunPollsMetricsWhileReportIsBlocked(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	metrics := NewMetrics()
	release := make(chan struct{})
	releaseRequest := sync.OnceFunc(func() { close(release) })
	defer releaseRequest()
	httpClient := &blockingHTTPClient{release: release}
	agent := NewAgent(
		metrics,
		NewClient("http://localhost", httpClient, "", nil),
		time.Millisecond,
		time.Millisecond,
		1,
	)
	agent.systemCollector = staticSystemMetricsCollector{}
	done := make(chan error, 1)

	go func() {
		done <- agent.Run(ctx)
	}()

	require.Eventually(t, func() bool {
		return httpClient.current.Load() == 1
	}, time.Second, time.Millisecond)
	before := pollCount(metrics)
	require.Eventually(t, func() bool {
		return pollCount(metrics) > before
	}, time.Second, time.Millisecond)

	cancel()
	releaseRequest()
	require.NoError(t, waitAgentStopped(t, done))
}

func TestAgentRunLimitsConcurrentReports(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	metrics := NewMetrics()
	metrics.gauges["TestGauge"] = 67.1
	release := make(chan struct{})
	releaseRequest := sync.OnceFunc(func() { close(release) })
	defer releaseRequest()
	httpClient := &blockingHTTPClient{release: release}
	agent := NewAgent(
		metrics,
		NewClient("http://localhost", httpClient, "", nil),
		time.Hour,
		time.Millisecond,
		2,
	)
	done := make(chan error, 1)

	go func() {
		done <- agent.Run(ctx)
	}()

	require.Eventually(t, func() bool {
		return httpClient.current.Load() == 2
	}, time.Second, time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	require.Equal(t, int32(2), httpClient.maximum.Load())
	require.Equal(t, int32(2), httpClient.requests.Load())

	cancel()
	releaseRequest()
	require.NoError(t, waitAgentStopped(t, done))
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
	done := make(chan error, 1)

	go func() {
		done <- agent.Run(ctx)
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
	require.NoError(t, waitAgentStopped(t, done))
}

func TestAgentRunPollsRuntimeWhileSystemCollectionIsBlocked(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	metrics := NewMetrics()
	release := make(chan struct{})
	releaseCollection := sync.OnceFunc(func() { close(release) })
	defer releaseCollection()
	systemCollector := &blockingSystemMetricsCollector{
		started: make(chan struct{}),
		release: release,
	}
	agent := NewAgent(
		metrics,
		NewClient("http://localhost", &recordingHTTPClient{}, "", nil),
		time.Millisecond,
		time.Hour,
		1,
	)
	agent.systemCollector = systemCollector
	done := make(chan error, 1)

	go func() {
		done <- agent.Run(ctx)
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
	releaseCollection()
	require.NoError(t, waitAgentStopped(t, done))
}

func pollCount(metrics *Metrics) int64 {
	metrics.mu.RLock()
	defer metrics.mu.RUnlock()
	return metrics.counters["PollCount"]
}

func waitAgentStopped(t *testing.T, done <-chan error) error {
	t.Helper()

	select {
	case err := <-done:
		return err
	case <-time.After(time.Second):
		t.Fatal("expected agent to stop after context cancellation")
		return nil
	}
}

type recordingHTTPClient struct {
	requests atomic.Int32
	err      error
}

func (c *recordingHTTPClient) Do(request *http.Request) (*http.Response, error) {
	c.requests.Add(1)
	defer request.Body.Close()
	if err := request.Context().Err(); err != nil {
		return nil, err
	}
	if c.err != nil {
		return nil, c.err
	}

	_, _ = io.Copy(io.Discard, request.Body)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{jsonContentType},
		},
		Body: io.NopCloser(strings.NewReader("[]")),
	}, nil
}

type blockingHTTPClient struct {
	requests    atomic.Int32
	current     atomic.Int32
	maximum     atomic.Int32
	started     chan struct{}
	startedOnce sync.Once
	release     <-chan struct{}
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

	if c.started != nil {
		c.startedOnce.Do(func() { close(c.started) })
	}
	select {
	case <-request.Context().Done():
		return nil, request.Context().Err()
	case <-c.release:
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{jsonContentType}},
			Body:       io.NopCloser(strings.NewReader("[]")),
		}, nil
	}
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
	release     <-chan struct{}
	values      systemMetrics
	calls       atomic.Int32
}

func (c *blockingSystemMetricsCollector) collect(ctx context.Context) (systemMetrics, error) {
	c.calls.Add(1)
	c.startedOnce.Do(func() {
		close(c.started)
	})
	select {
	case <-ctx.Done():
		return systemMetrics{}, ctx.Err()
	case <-c.release:
		return c.values, nil
	}
}
