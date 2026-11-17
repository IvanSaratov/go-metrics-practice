package agent

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/IvanSaratov/go-metrics-practice/internal/cryptoutil"
	"github.com/IvanSaratov/go-metrics-practice/internal/model"
	retrylib "github.com/sethvargo/go-retry"
	"github.com/stretchr/testify/require"
)

func TestClientSendGauge(t *testing.T) {
	var requestMethod string
	var requestPath string
	var contentType string
	var contentEncoding string
	var acceptEncoding string
	var metric model.Metrics
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

	client := NewClient(server.URL, compressionDisabledClient(), "", nil)

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
	require.Equal(t, model.Gauge, metric.MType)
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
	var metric model.Metrics
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

	client := NewClient(server.URL, compressionDisabledClient(), "", nil)

	err := client.SendCounter("TestCounter", 10)

	require.NoError(t, err)
	require.NoError(t, decodeErr)
	require.Equal(t, http.MethodPost, requestMethod)
	require.Equal(t, "/update", requestPath)
	require.Equal(t, "application/json", contentType)
	require.Equal(t, "gzip", contentEncoding)
	require.Equal(t, "gzip", acceptEncoding)
	require.Equal(t, "TestCounter", metric.ID)
	require.Equal(t, model.Counter, metric.MType)
	require.NotNil(t, metric.Delta)
	require.Equal(t, int64(10), *metric.Delta)
	require.Nil(t, metric.Value)
}

func TestClientSendBatch(t *testing.T) {
	gaugeValue := 67.1
	counterDelta := int64(10)
	want := []model.Metrics{
		{ID: "TestGauge", MType: model.Gauge, Value: &gaugeValue},
		{ID: "TestCounter", MType: model.Counter, Delta: &counterDelta},
	}

	var requestMethod string
	var requestPath string
	var contentType string
	var contentEncoding string
	var acceptEncoding string
	var metrics []model.Metrics
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

	client := NewClient(server.URL, compressionDisabledClient(), "", nil)

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

func TestClientEncryptsRequests(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	gaugeValue := 67.1
	counterDelta := int64(10)
	gauge := model.Metrics{ID: "TestGauge", MType: model.Gauge, Value: &gaugeValue}
	counter := model.Metrics{ID: "TestCounter", MType: model.Counter, Delta: &counterDelta}
	batch := []model.Metrics{gauge, counter}

	for _, tt := range []struct {
		name string
		path string
		want any
		send func(*Client) error
	}{
		{"gauge", "/update", gauge, func(c *Client) error { return c.SendGauge(gauge.ID, gaugeValue) }},
		{"counter", "/update", counter, func(c *Client) error { return c.SendCounter(counter.ID, counterDelta) }},
		{"batch", "/updates/", batch, func(c *Client) error { return c.SendBatch(context.Background(), batch) }},
	} {
		for _, key := range []string{"", "secret"} {
			t.Run(tt.name+"/key="+key, func(t *testing.T) {
				var requestBody []byte
				var headers http.Header
				var method, path string
				var readErr error
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requestBody, readErr = io.ReadAll(r.Body)
					headers = r.Header.Clone()
					method, path = r.Method, r.URL.Path
					w.Header().Set("Content-Type", "application/json")
				}))
				defer server.Close()
				client := NewClient(server.URL, compressionDisabledClient(), key, &privateKey.PublicKey)

				require.NoError(t, tt.send(client))
				require.NoError(t, readErr)
				require.Equal(t, http.MethodPost, method)
				require.Equal(t, tt.path, path)
				require.Equal(t, "application/json", headers.Get("Content-Type"))
				require.Equal(t, "gzip, rsa-aesgcm", headers.Get("Content-Encoding"))
				require.Equal(t, "gzip", headers.Get("Accept-Encoding"))

				compressed, err := cryptoutil.Decrypt(privateKey, requestBody)
				require.NoError(t, err)
				reader, err := gzip.NewReader(bytes.NewReader(compressed))
				require.NoError(t, err)
				defer reader.Close()
				body, err := io.ReadAll(reader)
				require.NoError(t, err)
				wantJSON, err := json.Marshal(tt.want)
				require.NoError(t, err)
				require.JSONEq(t, string(wantJSON), string(body))

				if key == "" {
					require.Empty(t, headers.Get("HashSHA256"))
				} else {
					hash := hmac.New(sha256.New, []byte(key))
					_, err = hash.Write(requestBody)
					require.NoError(t, err)
					require.Equal(t, hex.EncodeToString(hash.Sum(nil)), headers.Get("HashSHA256"))
				}
			})
		}
	}
}

func TestClientDoesNotSendOnEncryptionError(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
	}))
	defer server.Close()
	client := NewClient(server.URL, compressionDisabledClient(), "", &rsa.PublicKey{})

	err := client.SendGauge("TestGauge", 67.1)

	require.Error(t, err)
	require.Zero(t, requests.Load())
}

func TestClientSignsCompressedRequestBody(t *testing.T) {
	const key = "secret"

	var (
		requestBody []byte
		requestHash string
		readErr     error
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestBody, readErr = io.ReadAll(r.Body)
		requestHash = r.Header.Get("HashSHA256")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, compressionDisabledClient(), key, nil)

	err := client.SendGauge("TestGauge", 67.1)

	require.NoError(t, err)
	require.NoError(t, readErr)
	hash := hmac.New(sha256.New, []byte(key))
	_, err = hash.Write(requestBody)
	require.NoError(t, err)
	require.Equal(t, hex.EncodeToString(hash.Sum(nil)), requestHash)
}

func TestClientOmitsSignatureWithoutKey(t *testing.T) {
	var requestHash string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestHash = r.Header.Get("HashSHA256")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, compressionDisabledClient(), "", nil)

	err := client.SendGauge("TestGauge", 67.1)

	require.NoError(t, err)
	require.Empty(t, requestHash)
}

func TestClientRetriesTransportErrors(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	gaugeValue := 67.1
	want := []model.Metrics{
		{ID: "TestGauge", MType: model.Gauge, Value: &gaugeValue},
	}
	for _, encrypted := range []bool{false, true} {
		t.Run(fmt.Sprintf("encrypted=%t", encrypted), func(t *testing.T) {
			httpClient := &flakyHTTPClient{
				failures: 3,
				failureErr: &net.OpError{
					Op:  "dial",
					Net: "tcp",
					Err: errors.New("connection refused"),
				},
			}
			var publicKey *rsa.PublicKey
			if encrypted {
				publicKey = &privateKey.PublicKey
				httpClient.privateKey = privateKey
			}
			client := NewClient("http://localhost", httpClient, "secret", publicKey)
			client.retry = retryWithoutDelay

			err := client.SendBatch(context.Background(), want)

			require.NoError(t, err)
			require.Len(t, httpClient.received, 4)
			require.NotEmpty(t, httpClient.hashes[0])
			for i, metrics := range httpClient.received {
				require.Equal(t, want, metrics)
				require.Equal(t, httpClient.bodies[0], httpClient.bodies[i])
				require.Equal(t, httpClient.hashes[0], httpClient.hashes[i])
			}
		})
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
	client := NewClient("http://localhost", httpClient, "", nil)

	err := client.SendBatch(ctx, []model.Metrics{
		{ID: "TestGauge", MType: model.Gauge, Value: &gaugeValue},
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
	client := NewClient("http://localhost", httpClient, "", nil)
	client.retry = retryWithoutDelay

	err := client.SendBatch(context.Background(), []model.Metrics{
		{ID: "TestGauge", MType: model.Gauge, Value: &gaugeValue},
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

	client := NewClient(server.URL, compressionDisabledClient(), "", nil)

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

	client := NewClient(server.URL, server.Client(), "", nil)

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

	client := NewClient(server.URL, compressionDisabledClient(), "", nil)

	err := client.SendGauge("TestGauge", 67.1)

	require.Error(t, err)
	require.ErrorContains(t, err, "decode gzip response")
}

func TestClientRejectsCorruptedGzipResponse(t *testing.T) {
	var compressed bytes.Buffer
	require.NoError(t, writeGzipMetric(&compressed, model.Metrics{
		ID:    "TestGauge",
		MType: model.Gauge,
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

	client := NewClient(server.URL, compressionDisabledClient(), "", nil)

	err := client.SendGauge("TestGauge", 67.1)

	require.Error(t, err)
	require.ErrorContains(t, err, "read response body")
}

func TestEncodeGzipJSON(t *testing.T) {
	value := 67.1
	delta := int64(10)
	metrics := []model.Metrics{
		{ID: "TestGauge", MType: model.Gauge, Value: &value},
		{ID: "TestCounter", MType: model.Counter, Delta: &delta},
	}

	t.Run("Repeated", func(t *testing.T) {
		bodies := make([][]byte, len(metrics))
		for i, metric := range metrics {
			body, err := encodeGzipJSON(metric)
			require.NoError(t, err)
			bodies[i] = body
		}

		// Распаковываем после всех вызовов, чтобы заметить перезапись предыдущего результата.
		for i, body := range bodies {
			got, err := decodeGzipMetric(bytes.NewReader(body))
			require.NoError(t, err)
			require.Equal(t, metrics[i], got)
		}
	})

	t.Run("AfterError", func(t *testing.T) {
		body, err := encodeGzipJSON(math.NaN())
		require.Error(t, err)
		require.Nil(t, body)

		body, err = encodeGzipJSON(metrics[0])
		require.NoError(t, err)
		got, err := decodeGzipMetric(bytes.NewReader(body))
		require.NoError(t, err)
		require.Equal(t, metrics[0], got)
	})

	t.Run("Parallel", func(t *testing.T) {
		for i := range 8 {
			t.Run(fmt.Sprintf("worker_%d", i), func(t *testing.T) {
				t.Parallel()
				for j := range 10 {
					delta := int64(i*10 + j)
					want := model.Metrics{
						ID:    fmt.Sprintf("counter_%d", i),
						MType: model.Counter,
						Delta: &delta,
					}
					body, err := encodeGzipJSON(want)
					require.NoError(t, err)
					got, err := decodeGzipMetric(bytes.NewReader(body))
					require.NoError(t, err)
					require.Equal(t, want, got)
				}
			})
		}
	})
}

func decodeGzipMetric(body io.Reader) (model.Metrics, error) {
	reader, err := gzip.NewReader(body)
	if err != nil {
		return model.Metrics{}, err
	}

	var metric model.Metrics
	decodeErr := json.NewDecoder(reader).Decode(&metric)
	_, readErr := io.Copy(io.Discard, reader)
	return metric, errors.Join(decodeErr, readErr, reader.Close())
}

func decodeGzipMetrics(body io.Reader) ([]model.Metrics, error) {
	reader, err := gzip.NewReader(body)
	if err != nil {
		return nil, err
	}

	var metrics []model.Metrics
	decodeErr := json.NewDecoder(reader).Decode(&metrics)
	_, readErr := io.Copy(io.Discard, reader)
	return metrics, errors.Join(decodeErr, readErr, reader.Close())
}

func writeGzipMetric(w io.Writer, metric model.Metrics) error {
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
	received   [][]model.Metrics
	onRequest  func()
	privateKey *rsa.PrivateKey
	bodies     [][]byte
	hashes     []string
}

func (c *flakyHTTPClient) Do(request *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(request.Body)
	_ = request.Body.Close()
	if err != nil {
		return nil, err
	}
	c.bodies = append(c.bodies, body)
	c.hashes = append(c.hashes, request.Header.Get("HashSHA256"))
	if c.privateKey != nil {
		body, err = cryptoutil.Decrypt(c.privateKey, body)
		if err != nil {
			return nil, err
		}
	}
	metrics, err := decodeGzipMetrics(bytes.NewReader(body))
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
	return retrylib.Do(ctx, backoff, operation)
}
