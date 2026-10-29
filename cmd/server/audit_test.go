package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/IvanSaratov/go-metrics-practice/internal/audit"
	"github.com/IvanSaratov/go-metrics-practice/internal/handler"
	"github.com/IvanSaratov/go-metrics-practice/internal/repository"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestNewAuditConfiguresReceivers(t *testing.T) {
	fileErr := errors.New("audit write failed")
	clientErr := errors.New("audit connection failed")
	for _, tt := range []struct {
		name       string
		file       string
		url        string
		writeErr   error
		clientErr  error
		wantErrors []error
	}{
		{name: "disabled"},
		{name: "file only", file: "audit.jsonl"},
		{name: "URL only", url: "https://example.com/audit"},
		{name: "both receivers", file: "audit.jsonl", url: "https://example.com/audit"},
		{name: "file failure", file: "audit.jsonl", url: "https://example.com/audit", writeErr: fileErr, wantErrors: []error{fileErr}},
		{name: "URL failure", file: "audit.jsonl", url: "https://example.com/audit", clientErr: clientErr, wantErrors: []error{clientErr}},
		{name: "both failures", file: "audit.jsonl", url: "https://example.com/audit", writeErr: fileErr, clientErr: clientErr, wantErrors: []error{fileErr, clientErr}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			core, logs := observer.New(zapcore.DebugLevel)
			file := &serverAuditFile{writeErr: tt.writeErr}
			opened := false
			openFile := func(path string) (io.WriteCloser, error) {
				opened = true
				require.Equal(t, tt.file, path)
				return file, nil
			}
			var sentEvents []audit.Event
			client := serverAuditHTTPClient(func(request *http.Request) (*http.Response, error) {
				defer request.Body.Close()
				require.Equal(t, tt.url, request.URL.String())
				var event audit.Event
				require.NoError(t, json.NewDecoder(request.Body).Decode(&event))
				sentEvents = append(sentEvents, event)
				if tt.clientErr != nil {
					return nil, tt.clientErr
				}
				return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader(""))}, nil
			})

			notify, closer, err := newAudit(serverConfig{auditFile: tt.file, auditURL: tt.url}, zap.New(core), openFile, client)

			require.NoError(t, err)
			require.Equal(t, tt.file != "", opened)
			if tt.file == "" && tt.url == "" {
				require.Nil(t, notify)
				require.Nil(t, closer)
				require.Empty(t, sentEvents)
				return
			}
			require.NotNil(t, notify)
			event := audit.Event{Timestamp: 12345678, Metrics: []string{"Alloc"}, IPAddress: "192.168.0.42"}
			notify(context.Background(), event)
			if tt.file != "" && tt.writeErr == nil {
				var written audit.Event
				require.NoError(t, json.Unmarshal(file.Bytes(), &written))
				require.Equal(t, event, written)
			} else {
				require.Zero(t, file.Len())
			}
			if tt.url != "" {
				require.Equal(t, []audit.Event{event}, sentEvents)
			} else {
				require.Empty(t, sentEvents)
			}
			entries := logs.FilterLevelExact(zapcore.ErrorLevel).All()
			if len(tt.wantErrors) == 0 {
				require.Empty(t, entries)
			} else {
				require.Len(t, entries, 1)
				for _, wantErr := range tt.wantErrors {
					require.Contains(t, entries[0].ContextMap()["error"], wantErr.Error())
				}
			}
			if tt.file != "" {
				require.NotNil(t, closer)
				require.NoError(t, closer.Close())
				require.True(t, file.closed)
			} else {
				require.Nil(t, closer)
			}
		})
	}
}

func TestNewAuditRejectsInvalidConfiguration(t *testing.T) {
	openErr := errors.New("audit file unavailable")
	client := serverAuditHTTPClient(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("unexpected HTTP request")
	})
	for _, tt := range []struct {
		name     string
		url      string
		openErr  error
		wantOpen bool
	}{
		{name: "invalid URL", url: "/audit"},
		{name: "file open failure", url: "https://example.com/audit", openErr: openErr, wantOpen: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			opened := false
			notify, closer, err := newAudit(serverConfig{auditFile: "audit.jsonl", auditURL: tt.url}, zap.NewNop(),
				func(string) (io.WriteCloser, error) {
					opened = true
					if tt.openErr != nil {
						return nil, tt.openErr
					}
					return &serverAuditFile{}, nil
				}, client)

			require.Error(t, err)
			if tt.openErr != nil {
				require.ErrorIs(t, err, tt.openErr)
			}
			require.Equal(t, tt.wantOpen, opened)
			require.Nil(t, notify)
			require.Nil(t, closer)
		})
	}
}

// Редирект может превратить POST в GET, поэтому не переходим по нему.
func TestAuditHTTPClientDoesNotFollowRedirects(t *testing.T) {
	client := newAuditHTTPClient()
	requests := 0
	client.Transport = serverAuditTransport(func(request *http.Request) (*http.Response, error) {
		requests++
		require.Equal(t, http.MethodPost, request.Method)
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": []string{"https://example.com/other"}},
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	})
	request, err := http.NewRequest(http.MethodPost, "https://example.com/audit", strings.NewReader(`{}`))
	require.NoError(t, err)

	response, err := client.Do(request)

	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusFound, response.StatusCode)
	require.Equal(t, 1, requests)
}

func TestServerHandlerKeepsSuccessWhenAuditFails(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	appLogger := zap.New(core)
	notify, closer, err := newAudit(serverConfig{
		auditFile: "audit.jsonl",
		auditURL:  "https://example.com/audit",
	}, appLogger, func(string) (io.WriteCloser, error) {
		return &serverAuditFile{writeErr: errors.New("audit write failed")}, nil
	}, serverAuditHTTPClient(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("audit connection failed")
	}))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })
	storage := repository.NewMemStorage()
	server := handler.NewServer(storage, nil, notify)
	request := httptest.NewRequest(http.MethodPost, "/update", strings.NewReader(`{"id":"Alloc","type":"gauge","value":12.5}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	withMiddleware(server, appLogger, "").ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	value, found, err := storage.GetGauge(context.Background(), "Alloc")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, 12.5, value)
	require.Len(t, logs.FilterLevelExact(zapcore.ErrorLevel).All(), 1)
}

type serverAuditFile struct {
	bytes.Buffer
	writeErr error
	closed   bool
}

func (f *serverAuditFile) Write(data []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return f.Buffer.Write(data)
}

func (f *serverAuditFile) Close() error {
	f.closed = true
	return nil
}

type serverAuditHTTPClient func(*http.Request) (*http.Response, error)

func (f serverAuditHTTPClient) Do(request *http.Request) (*http.Response, error) {
	return f(request)
}

type serverAuditTransport func(*http.Request) (*http.Response, error)

func (f serverAuditTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
