package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	models "github.com/IvanSaratov/go-metrics-practice/internal/model"
	"github.com/stretchr/testify/require"
)

func TestClientSendGauge(t *testing.T) {
	var requestMethod string
	var requestPath string
	var contentType string
	var metric models.Metrics
	var decodeErr error

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestMethod = r.Method
		requestPath = r.URL.Path
		contentType = r.Header.Get("Content-Type")
		decodeErr = json.NewDecoder(r.Body).Decode(&metric)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(metric)
	}))
	defer server.Close()

	client := NewClient(server.URL, server.Client())

	err := client.SendGauge("TestGauge", 67.1)

	require.NoError(t, err)
	require.NoError(t, decodeErr)
	require.Equal(t, http.MethodPost, requestMethod)
	require.Equal(t, "/update", requestPath)
	require.Equal(t, "application/json", contentType)
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
	var metric models.Metrics
	var decodeErr error

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestMethod = r.Method
		requestPath = r.URL.Path
		contentType = r.Header.Get("Content-Type")
		decodeErr = json.NewDecoder(r.Body).Decode(&metric)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(metric)
	}))
	defer server.Close()

	client := NewClient(server.URL, server.Client())

	err := client.SendCounter("TestCounter", 10)

	require.NoError(t, err)
	require.NoError(t, decodeErr)
	require.Equal(t, http.MethodPost, requestMethod)
	require.Equal(t, "/update", requestPath)
	require.Equal(t, "application/json", contentType)
	require.Equal(t, "TestCounter", metric.ID)
	require.Equal(t, models.Counter, metric.MType)
	require.NotNil(t, metric.Delta)
	require.Equal(t, int64(10), *metric.Delta)
	require.Nil(t, metric.Value)
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
