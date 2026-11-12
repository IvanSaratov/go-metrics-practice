package middleware

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"testing/iotest"

	"github.com/IvanSaratov/go-metrics-practice/internal/cryptoutil"
	"github.com/stretchr/testify/require"
)

func TestCryptoMiddleware(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	payload := []byte(`{"id":"TestGauge","type":"gauge","value":67.1}`)
	compressed := gzipBytes(t, payload)

	for _, tt := range []struct {
		name         string
		encodings    []string
		plaintext    []byte
		wantEncoding string
	}{
		{"encrypted JSON", []string{"rsa-aesgcm"}, payload, ""},
		{"encrypted gzip", []string{"gzip, rsa-aesgcm"}, compressed, "gzip"},
		{"case and whitespace", []string{" GZIP , RSA-AESGCM "}, compressed, "gzip"},
		{"separate headers", []string{"gzip", "rsa-aesgcm"}, compressed, "gzip"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ciphertext, err := cryptoutil.Encrypt(&privateKey.PublicKey, tt.plaintext)
			require.NoError(t, err)
			body := &cryptoTestBody{Reader: bytes.NewReader(ciphertext)}
			request := httptest.NewRequest(http.MethodPost, "/update", body)
			request.Header["Content-Encoding"] = tt.encodings
			request.ContentLength = int64(len(ciphertext))
			request.Header.Set("Content-Length", strconv.Itoa(len(ciphertext)))
			request.GetBody = func() (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(ciphertext)), nil
			}
			response := httptest.NewRecorder()
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.Equal(t, tt.plaintext, data)
				require.Equal(t, tt.wantEncoding, r.Header.Get("Content-Encoding"))
				require.Equal(t, int64(len(data)), r.ContentLength)
				require.Equal(t, strconv.Itoa(len(data)), r.Header.Get("Content-Length"))
				require.Nil(t, r.GetBody)
				require.True(t, body.closed)
				w.WriteHeader(http.StatusNoContent)
			})

			CryptoMiddleware(privateKey)(next).ServeHTTP(response, request)

			require.Equal(t, http.StatusNoContent, response.Code)
		})
	}
}

func TestCryptoMiddlewarePassesUnencryptedRequests(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	for _, tt := range []struct {
		name       string
		method     string
		encoding   string
		body       []byte
		privateKey *rsa.PrivateKey
	}{
		{"without key", http.MethodPost, "", []byte("plain body"), nil},
		{"with key", http.MethodPost, "", []byte("plain body"), privateKey},
		{"identity", http.MethodPost, "identity", []byte("plain body"), privateKey},
		{"gzip", http.MethodPost, "gzip", gzipBytes(t, []byte("plain body")), privateKey},
		{"unknown encoding", http.MethodPost, "br", []byte("encoded body"), privateKey},
		{"GET without body", http.MethodGet, "", nil, privateKey},
	} {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(tt.method, "/ping", bytes.NewReader(tt.body))
			request.Header.Set("Content-Encoding", tt.encoding)
			originalBody := request.Body
			response := httptest.NewRecorder()
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.True(t, originalBody == r.Body)
				data, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.Equal(t, string(tt.body), string(data))
				require.Equal(t, tt.encoding, r.Header.Get("Content-Encoding"))
				require.Equal(t, int64(len(tt.body)), r.ContentLength)
				w.WriteHeader(http.StatusNoContent)
			})

			CryptoMiddleware(tt.privateKey)(next).ServeHTTP(response, request)

			require.Equal(t, http.StatusNoContent, response.Code)
		})
	}
}

func TestCryptoMiddlewareRejectsInvalidRequests(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	ciphertext, err := cryptoutil.Encrypt(&privateKey.PublicKey, []byte("request body"))
	require.NoError(t, err)
	corrupted := bytes.Clone(ciphertext)
	corrupted[len(corrupted)-1] ^= 1

	for _, tt := range []struct {
		name       string
		privateKey *rsa.PrivateKey
		body       io.Reader
	}{
		{"missing key", nil, bytes.NewReader(ciphertext)},
		{"wrong key", otherKey, bytes.NewReader(ciphertext)},
		{"corrupted body", privateKey, bytes.NewReader(corrupted)},
		{"empty body", privateKey, bytes.NewReader(nil)},
		{"read error", privateKey, iotest.ErrReader(errors.New("body read failed"))},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := &cryptoTestBody{Reader: tt.body}
			request := httptest.NewRequest(http.MethodPost, "/update", body)
			request.Header.Set("Content-Encoding", "gzip, rsa-aesgcm")
			response := httptest.NewRecorder()
			called := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
			})

			CryptoMiddleware(tt.privateKey)(next).ServeHTTP(response, request)

			require.Equal(t, http.StatusBadRequest, response.Code)
			require.Equal(t, "invalid encrypted body\n", response.Body.String())
			require.False(t, called)
			require.True(t, body.closed)
			if tt.privateKey == nil {
				require.False(t, body.read)
			}
		})
	}
}

type cryptoTestBody struct {
	io.Reader
	closed bool
	read   bool
}

func (b *cryptoTestBody) Read(p []byte) (int, error) {
	b.read = true
	return b.Reader.Read(p)
}

func (b *cryptoTestBody) Close() error {
	b.closed = true
	return nil
}
