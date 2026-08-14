package agent

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	models "github.com/IvanSaratov/go-metrics-practice/internal/model"
	retrylib "github.com/sethvargo/go-retry"
	"github.com/stretchr/testify/require"
)

func TestClientSendGauge(t *testing.T) {
	var requestMethod string
	var requestPath string
	var contentType string
	var contentEncoding string
	var acceptEncoding string
	var metric models.Metrics
	var decodeErr error
	var responseErr error

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestMethod = r.Method
		requestPath = r.URL.Path
		contentType = r.Header.Get("Content-Type")
		contentEncoding = r.Header.Get("Content-Encoding")
		acceptEncoding = r.Header.Get("Accept-Encoding")
		metric, decodeErr = decodeGzipMetric(r.Body)

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(http.StatusOK)
		responseErr = writeGzipMetric(w, metric)
	}))
	defer server.Close()

	client := NewClient(server.URL, compressionDisabledClient())

	err := client.SendGauge("TestGauge", 67.1)

	require.NoError(t, err)
	require.NoError(t, decodeErr)
	require.NoError(t, responseErr)
	require.Equal(t, http.MethodPost, requestMethod)
	require.Equal(t, "/update", requestPath)
	require.Equal(t, "application/json", contentType)
	require.Equal(t, "gzip", contentEncoding)
	require.Equal(t, "gzip", acceptEncoding)
	require.Equal(t, "TestGauge", metric.ID)
	require.Equal(t, models.Gauge, metric.MType)
	require.NotNil(t, metric.Value)
	require.Equal(t, 67.1, *metric.Value)
	require.Nil(t, metric.Delta)
}

func TestClientSendCounter(t *testing.T) {
	var requestMethod string
	var requestPath string
	var contentType string
	var contentEncoding string
	var acceptEncoding string
	var metric models.Metrics
	var decodeErr error

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestMethod = r.Method
		requestPath = r.URL.Path
		contentType = r.Header.Get("Content-Type")
		contentEncoding = r.Header.Get("Content-Encoding")
		acceptEncoding = r.Header.Get("Accept-Encoding")
		metric, decodeErr = decodeGzipMetric(r.Body)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(metric)
	}))
	defer server.Close()

	client := NewClient(server.URL, compressionDisabledClient())

	err := client.SendCounter("TestCounter", 10)

	require.NoError(t, err)
	require.NoError(t, decodeErr)
	require.Equal(t, http.MethodPost, requestMethod)
	require.Equal(t, "/update", requestPath)
	require.Equal(t, "application/json", contentType)
	require.Equal(t, "gzip", contentEncoding)
	require.Equal(t, "gzip", acceptEncoding)
	require.Equal(t, "TestCounter", metric.ID)
	require.Equal(t, models.Counter, metric.MType)
	require.NotNil(t, metric.Delta)
	require.Equal(t, int64(10), *metric.Delta)
	require.Nil(t, metric.Value)
}

func TestClientSendBatch(t *testing.T) {
	gaugeValue := 67.1
	counterDelta := int64(10)
	want := []models.Metrics{
		{ID: "TestGauge", MType: models.Gauge, Value: &gaugeValue},
		{ID: "TestCounter", MType: models.Counter, Delta: &counterDelta},
	}

	var requestMethod string
	var requestPath string
	var contentType string
	var contentEncoding string
	var acceptEncoding string
	var metrics []models.Metrics
	var decodeErr error

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestMethod = r.Method
		requestPath = r.URL.Path
		contentType = r.Header.Get("Content-Type")
		contentEncoding = r.Header.Get("Content-Encoding")
		acceptEncoding = r.Header.Get("Accept-Encoding")
		metrics, decodeErr = decodeGzipMetrics(r.Body)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(metrics)
	}))
	defer server.Close()

	client := NewClient(server.URL, compressionDisabledClient())

	err := client.SendBatch(context.Background(), want)

	require.NoError(t, err)
	require.NoError(t, decodeErr)
	require.Equal(t, http.MethodPost, requestMethod)
	require.Equal(t, "/updates/", requestPath)
	require.Equal(t, "application/json", contentType)
	require.Equal(t, "gzip", contentEncoding)
	require.Equal(t, "gzip", acceptEncoding)
	require.Equal(t, want, metrics)
}

func TestClientRetriesTransportErrors(t *testing.T) {
	gaugeValue := 67.1
	want := []models.Metrics{
		{ID: "TestGauge", MType: models.Gauge, Value: &gaugeValue},
	}
	httpClient := &flakyHTTPClient{
		failures: 3,
		failureErr: &net.OpError{
			Op:  "dial",
			Net: "tcp",
			Err: errors.New("connection refused"),
		},
	}
	client := NewClient("http://localhost", httpClient)
	client.retry = retryWithoutDelay

	err := client.SendBatch(context.Background(), want)

	require.NoError(t, err)
	require.Len(t, httpClient.received, 4)
	for _, metrics := range httpClient.received {
		require.Equal(t, want, metrics)
	}
}

func TestClientStopsRetryWhenContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	gaugeValue := 67.1
	httpClient := &flakyHTTPClient{
		failures: 1,
		failureErr: &net.OpError{
			Op:  "dial",
			Net: "tcp",
			Err: errors.New("connection refused"),
		},
		onRequest: func() {
			cancel()
		},
	}
	client := NewClient("http://localhost", httpClient)

	err := client.SendBatch(ctx, []models.Metrics{
		{ID: "TestGauge", MType: models.Gauge, Value: &gaugeValue},
	})

	require.ErrorIs(t, err, context.Canceled)
	require.Len(t, httpClient.received, 1)
}

func TestClientDoesNotRetryOtherErrors(t *testing.T) {
	gaugeValue := 67.1
	httpClient := &flakyHTTPClient{
		failures:   4,
		failureErr: errors.New("request failed after connection"),
	}
	client := NewClient("http://localhost", httpClient)
	client.retry = retryWithoutDelay

	err := client.SendBatch(context.Background(), []models.Metrics{
		{ID: "TestGauge", MType: models.Gauge, Value: &gaugeValue},
	})

	require.Error(t, err)
	require.Len(t, httpClient.received, 1)
}

func TestClientDoesNotSendEmptyBatch(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, compressionDisabledClient())

	err := client.SendBatch(context.Background(), nil)

	require.NoError(t, err)
	require.Zero(t, requests.Load())
}

func TestClientRejectsNonJSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, server.Client())

	err := client.SendGauge("TestGauge", 67.1)

	require.Error(t, err)
	require.ErrorContains(t, err, "unexpected Content-Type")
}

func TestClientRejectsInvalidGzipResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not gzip"))
	}))
	defer server.Close()

	client := NewClient(server.URL, compressionDisabledClient())

	err := client.SendGauge("TestGauge", 67.1)

	require.Error(t, err)
	require.ErrorContains(t, err, "decode gzip response")
}

func TestClientRejectsCorruptedGzipResponse(t *testing.T) {
	var compressed bytes.Buffer
	require.NoError(t, writeGzipMetric(&compressed, models.Metrics{
		ID:    "TestGauge",
		MType: models.Gauge,
	}))
	body := compressed.Bytes()
	body[len(body)-1] ^= 0xff

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	client := NewClient(server.URL, compressionDisabledClient())

	err := client.SendGauge("TestGauge", 67.1)

	require.Error(t, err)
	require.ErrorContains(t, err, "read response body")
}

func decodeGzipMetric(body io.Reader) (models.Metrics, error) {
	reader, err := gzip.NewReader(body)
	if err != nil {
		return models.Metrics{}, err
	}

	var metric models.Metrics
	decodeErr := json.NewDecoder(reader).Decode(&metric)
	_, readErr := io.Copy(io.Discard, reader)
	return metric, errors.Join(decodeErr, readErr, reader.Close())
}

func decodeGzipMetrics(body io.Reader) ([]models.Metrics, error) {
	reader, err := gzip.NewReader(body)
	if err != nil {
		return nil, err
	}

	var metrics []models.Metrics
	decodeErr := json.NewDecoder(reader).Decode(&metrics)
	_, readErr := io.Copy(io.Discard, reader)
	return metrics, errors.Join(decodeErr, readErr, reader.Close())
}

func writeGzipMetric(w io.Writer, metric models.Metrics) error {
	writer := gzip.NewWriter(w)
	encodeErr := json.NewEncoder(writer).Encode(metric)
	return errors.Join(encodeErr, writer.Close())
}

func compressionDisabledClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DisableCompression: true,
		},
	}
}

type flakyHTTPClient struct {
	failures   int
	failureErr error
	received   [][]models.Metrics
	onRequest  func()
}

func (c *flakyHTTPClient) Do(request *http.Request) (*http.Response, error) {
	metrics, err := decodeGzipMetrics(request.Body)
	_ = request.Body.Close()
	if err != nil {
		return nil, err
	}
	c.received = append(c.received, metrics)
	if c.onRequest != nil {
		c.onRequest()
	}

	if len(c.received) <= c.failures {
		return nil, c.failureErr
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{jsonContentType},
		},
		Body: io.NopCloser(strings.NewReader("[]")),
	}, nil
}

func retryWithoutDelay(ctx context.Context, operation func(context.Context) error) error {
	backoff := retrylib.WithMaxRetries(3, retrylib.NewConstant(time.Nanosecond))
	return retrylib.Do(ctx, backoff, retrylib.RetryFunc(operation))
}
