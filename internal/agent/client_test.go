package agent

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClientSendGauge(t *testing.T) {
	var requestMethod string
	var requestPath string
	var contentType string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestMethod = r.Method
		requestPath = r.URL.Path
		contentType = r.Header.Get("Content-Type")

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, server.Client())

	err := client.SendGauge("TestGauge", 67.1)

	require.NoError(t, err)
	require.Equal(t, http.MethodPost, requestMethod)
	require.Equal(t, "/update/gauge/TestGauge/67.1", requestPath)
	require.Equal(t, "text/plain", contentType)
}

func TestClientSendCounter(t *testing.T) {
	var requestMethod string
	var requestPath string
	var contentType string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestMethod = r.Method
		requestPath = r.URL.Path
		contentType = r.Header.Get("Content-Type")

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, server.Client())

	err := client.SendCounter("TestCounter", 10)

	require.NoError(t, err)
	require.Equal(t, http.MethodPost, requestMethod)
	require.Equal(t, "/update/counter/TestCounter/10", requestPath)
	require.Equal(t, "text/plain", contentType)
}
