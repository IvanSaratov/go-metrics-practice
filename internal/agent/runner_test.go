package agent

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
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
		NewClient("http://localhost", &recordingHTTPClient{}),
		time.Millisecond,
		time.Hour,
	)
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
	client := NewClient("http://localhost", httpClient)
	client.retry = retryWithoutDelay
	agent := NewAgent(
		metrics,
		client,
		time.Hour,
		time.Millisecond,
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

func TestAgentRunCancelsActiveReport(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	metrics := NewMetrics()
	metrics.gauges["TestGauge"] = 67.1
	client := NewClient("http://localhost", &recordingHTTPClient{})
	reportContexts := make(chan context.Context, 1)
	client.retry = func(reportCtx context.Context, _ func(context.Context) error) error {
		reportContexts <- reportCtx
		return errors.New("send batch failed")
	}
	agent := NewAgent(metrics, client, time.Hour, time.Millisecond)
	done := make(chan struct{})

	go func() {
		defer close(done)
		agent.Run(ctx)
	}()

	var reportCtx context.Context
	select {
	case reportCtx = <-reportContexts:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected agent to start reporting metrics")
	}
	cancel()

	select {
	case <-reportCtx.Done():
	case <-time.After(100 * time.Millisecond):
		t.Error("expected report context to be canceled")
	}
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Error("expected agent to stop")
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
