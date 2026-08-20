package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSignatureMiddlewareAllowsMatchingRequest(t *testing.T) {
	const (
		body = "request body"
		key  = "secret"
		hash = "284ecbd7ee5e3868010384e98f2f397a4d733937d14b0a19dcff5ae95739962b"
	)

	var receivedBody string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		receivedBody = string(data)
		w.WriteHeader(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodPost, "/update", bytes.NewBufferString(body))
	request.Header.Set("HashSHA256", hash)
	response := httptest.NewRecorder()

	SignatureMiddleware(key)(next).ServeHTTP(response, request)

	require.Equal(t, http.StatusNoContent, response.Code)
	require.Equal(t, body, receivedBody)
}

func TestSignatureMiddlewareRejectsInvalidRequest(t *testing.T) {
	const validHash = "284ecbd7ee5e3868010384e98f2f397a4d733937d14b0a19dcff5ae95739962b"

	tests := []struct {
		name string
		body string
		hash string
	}{
		{
			name: "changed body",
			body: "changed body",
			hash: validHash,
		},
		{
			name: "different signature",
			body: "request body",
			hash: "0000000000000000000000000000000000000000000000000000000000000000",
		},
		{
			name: "malformed hex",
			body: "request body",
			hash: "not-hex",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlerCalled := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				handlerCalled = true
			})
			request := httptest.NewRequest(
				http.MethodPost,
				"/update",
				bytes.NewBufferString(tt.body),
			)
			request.Header.Set("HashSHA256", tt.hash)
			response := httptest.NewRecorder()

			SignatureMiddleware("secret")(next).ServeHTTP(response, request)

			require.Equal(t, http.StatusBadRequest, response.Code)
			require.False(t, handlerCalled)
		})
	}
}

func TestSignatureMiddlewareAllowsRequestWithoutSignature(t *testing.T) {
	const responseHash = "58882f4e8e08ac7f68d5c1e3c2e8118664f9939315d95178161d45ae556d3034"

	var receivedBody string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		receivedBody = string(data)
		_, _ = w.Write([]byte("response body"))
	})
	request := httptest.NewRequest(
		http.MethodPost,
		"/update",
		bytes.NewBufferString("unsigned request"),
	)
	response := httptest.NewRecorder()

	SignatureMiddleware("secret")(next).ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "unsigned request", receivedBody)
	require.Equal(t, "response body", response.Body.String())
	require.Equal(t, responseHash, response.Header().Get("HashSHA256"))
}

func TestSignatureMiddlewareSignsResponseBody(t *testing.T) {
	const (
		requestHash  = "284ecbd7ee5e3868010384e98f2f397a4d733937d14b0a19dcff5ae95739962b"
		responseHash = "58882f4e8e08ac7f68d5c1e3c2e8118664f9939315d95178161d45ae556d3034"
	)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("response body"))
	})
	request := httptest.NewRequest(
		http.MethodPost,
		"/update",
		bytes.NewBufferString("request body"),
	)
	request.Header.Set("HashSHA256", requestHash)
	response := httptest.NewRecorder()

	SignatureMiddleware("secret")(next).ServeHTTP(response, request)

	require.Equal(t, http.StatusCreated, response.Code)
	require.Equal(t, "text/plain", response.Header().Get("Content-Type"))
	require.Equal(t, "response body", response.Body.String())
	require.Equal(t, responseHash, response.Header().Get("HashSHA256"))
}

func TestSignatureMiddlewareSignsBadRequestResponse(t *testing.T) {
	const responseHash = "c4d0fe45021b2f91173c52d7f330139beb0103630b6180afd12eb6f0f33d443c"

	request := httptest.NewRequest(
		http.MethodPost,
		"/update",
		bytes.NewBufferString("request body"),
	)
	request.Header.Set(
		"HashSHA256",
		"0000000000000000000000000000000000000000000000000000000000000000",
	)
	response := httptest.NewRecorder()

	SignatureMiddleware("secret")(http.NotFoundHandler()).ServeHTTP(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Equal(t, "invalid request signature\n", response.Body.String())
	require.Equal(t, responseHash, response.Header().Get("HashSHA256"))
}

func TestSignatureMiddlewareIsDisabledWithoutKey(t *testing.T) {
	var receivedBody string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		receivedBody = string(data)
		_, _ = w.Write([]byte("unsigned response"))
	})
	request := httptest.NewRequest(
		http.MethodPost,
		"/update",
		bytes.NewBufferString("unsigned request"),
	)
	response := httptest.NewRecorder()

	SignatureMiddleware("")(next).ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "unsigned request", receivedBody)
	require.Equal(t, "unsigned response", response.Body.String())
	require.Empty(t, response.Header().Get("HashSHA256"))
}
