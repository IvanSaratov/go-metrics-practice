package middleware

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGzipMiddlewareDecompressesRequest(t *testing.T) {
	const payload = `{"id":"TestGauge","type":"gauge","value":67.1}`

	var receivedBody string
	var receivedEncoding string
	var receivedLength int64
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		receivedBody = string(body)
		receivedEncoding = r.Header.Get("Content-Encoding")
		receivedLength = r.ContentLength
		w.WriteHeader(http.StatusNoContent)
	})
	request := httptest.NewRequest(
		http.MethodPost,
		"/update",
		bytes.NewReader(gzipBytes(t, []byte(payload))),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Content-Encoding", "gzip")
	response := httptest.NewRecorder()

	GzipMiddleware(next).ServeHTTP(response, request)

	require.Equal(t, http.StatusNoContent, response.Code)
	require.JSONEq(t, payload, receivedBody)
	require.Empty(t, receivedEncoding)
	require.Equal(t, int64(-1), receivedLength)
}

func TestGzipMiddlewareCompressesSupportedResponses(t *testing.T) {
	tests := []struct {
		name           string
		contentType    string
		acceptEncoding string
		body           string
	}{
		{
			name:           "JSON with gzip",
			contentType:    "application/json",
			acceptEncoding: "br, GZip; q=0.5",
			body:           `{"status":"ok"}`,
		},
		{
			name:           "HTML with wildcard",
			contentType:    "text/html; charset=utf-8",
			acceptEncoding: "*;q=0.5",
			body:           "<html><body>metrics</body></html>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tt.contentType)
				w.Header().Set("Content-Length", "1024")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(tt.body))
			})
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.Header.Set("Accept-Encoding", tt.acceptEncoding)
			response := httptest.NewRecorder()

			GzipMiddleware(next).ServeHTTP(response, request)

			require.Equal(t, http.StatusOK, response.Code)
			require.Equal(t, "gzip", response.Header().Get("Content-Encoding"))
			require.Contains(t, response.Header().Values("Vary"), "Accept-Encoding")
			require.Empty(t, response.Header().Get("Content-Length"))
			require.Equal(t, tt.body, readGzipBody(t, response.Body.Bytes()))
		})
	}
}

func TestGzipMiddlewareLeavesUnsupportedResponsesUncompressed(t *testing.T) {
	tests := []struct {
		name           string
		contentType    string
		acceptEncoding string
	}{
		{
			name:        "client does not accept gzip",
			contentType: "application/json",
		},
		{
			name:           "content type is not compressible",
			contentType:    "text/plain; charset=utf-8",
			acceptEncoding: "gzip",
		},
		{
			name:           "gzip is explicitly disabled",
			contentType:    "application/json",
			acceptEncoding: "*;q=1, gzip;q=0",
		},
		{
			name:           "different encoding contains gzip text",
			contentType:    "application/json",
			acceptEncoding: "xgzip",
		},
		{
			name:           "deflate is outside the middleware contract",
			contentType:    "application/json",
			acceptEncoding: "deflate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tt.contentType)
				_, _ = w.Write([]byte("plain response"))
			})
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.Header.Set("Accept-Encoding", tt.acceptEncoding)
			response := httptest.NewRecorder()

			GzipMiddleware(next).ServeHTTP(response, request)

			require.Equal(t, http.StatusOK, response.Code)
			require.Empty(t, response.Header().Get("Content-Encoding"))
			require.Equal(t, "plain response", response.Body.String())
		})
	}
}

func TestGzipMiddlewareRejectsInvalidGzipRequest(t *testing.T) {
	handlerCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	})
	request := httptest.NewRequest(
		http.MethodPost,
		"/update",
		bytes.NewBufferString("not gzip"),
	)
	request.Header.Set("Content-Encoding", "gzip")
	response := httptest.NewRecorder()

	GzipMiddleware(next).ServeHTTP(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
	require.False(t, handlerCalled)
}

func TestGzipMiddlewareRejectsUnsupportedRequestEncoding(t *testing.T) {
	handlerCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	})
	request := httptest.NewRequest(
		http.MethodPost,
		"/update",
		bytes.NewBufferString("payload"),
	)
	request.Header.Set("Content-Encoding", "br")
	response := httptest.NewRecorder()

	GzipMiddleware(next).ServeHTTP(response, request)

	require.Equal(t, http.StatusUnsupportedMediaType, response.Code)
	require.False(t, handlerCalled)
}

func TestGzipMiddlewareAcceptsUncompressedRequest(t *testing.T) {
	handlerCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusNoContent)
	})
	request := httptest.NewRequest(
		http.MethodPost,
		"/update",
		bytes.NewBufferString(`{"id":"TestGauge"}`),
	)
	response := httptest.NewRecorder()

	GzipMiddleware(next).ServeHTTP(response, request)

	require.Equal(t, http.StatusNoContent, response.Code)
	require.True(t, handlerCalled)
}

func gzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()

	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, err := writer.Write(data)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return compressed.Bytes()
}

func readGzipBody(t *testing.T, data []byte) string {
	t.Helper()

	reader, err := gzip.NewReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer reader.Close()

	body, err := io.ReadAll(reader)
	require.NoError(t, err)
	return string(body)
}
