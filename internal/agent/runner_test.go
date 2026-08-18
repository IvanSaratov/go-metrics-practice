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
