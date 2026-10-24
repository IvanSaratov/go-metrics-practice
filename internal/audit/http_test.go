package audit

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/stretchr/testify/require"
)

// Проверяем адрес приёмника до попытки отправить событие.
func TestNewHTTPObserverValidatesURL(t *testing.T) {
	client := auditHTTPClientFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("unexpected HTTP request")
	})
	for _, tt := range []struct {
		name      string
		url       string
		wantError bool
	}{
		{name: "HTTP", url: "http://localhost:9090/audit"},
		{name: "HTTPS", url: "https://example.com/audit?source=metrics"},
		{name: "empty", url: "", wantError: true},
		{name: "relative", url: "/audit", wantError: true},
		{name: "missing scheme", url: "example.com/audit", wantError: true},
		{name: "unsupported scheme", url: "ftp://example.com/audit", wantError: true},
		{name: "missing hostname", url: "http://:9090/audit", wantError: true},
		{name: "malformed", url: "http://%/audit", wantError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewHTTPObserver(tt.url, client)

			if tt.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// Проверяем содержимое запроса, ограничение времени и освобождение ответа.
func TestHTTPObserverUpdateSendsEvent(t *testing.T) {
	event := Event{
		Timestamp: 12345678,
		Metrics:   []string{"Alloc", "Frees"},
		IPAddress: "192.168.0.42",
	}
	const destination = "https://example.com/audit?source=metrics"
	responseReader := strings.NewReader("accepted")
	responseBody := &auditResponseBody{Reader: responseReader}
	var request *http.Request
	var received Event
	client := auditHTTPClientFunc(func(req *http.Request) (*http.Response, error) {
		request = req
		defer req.Body.Close()
		require.NoError(t, json.NewDecoder(req.Body).Decode(&received))
		return &http.Response{StatusCode: http.StatusOK, Body: responseBody}, nil
	})
	observer, err := NewHTTPObserver(destination, client)
	require.NoError(t, err)
	started := time.Now()

	err = observer.Update(context.Background(), event)

	require.NoError(t, err)
	require.NotNil(t, request)
	require.Equal(t, http.MethodPost, request.Method)
	require.Equal(t, destination, request.URL.String())
	require.Equal(t, "application/json", request.Header.Get("Content-Type"))
	require.Equal(t, event, received)
	deadline, ok := request.Context().Deadline()
	require.True(t, ok)
	require.False(t, deadline.Before(started.Add(5*time.Second)))
	require.False(t, deadline.After(time.Now().Add(5*time.Second)))
	require.Zero(t, responseReader.Len())
	require.True(t, responseBody.closed)
}

// Успешными считаются только ответы 2xx, тело ответа закрывается при любом статусе.
func TestHTTPObserverUpdateChecksStatus(t *testing.T) {
	for _, tt := range []struct {
		name      string
		status    int
		wantError bool
	}{
		{name: "OK", status: http.StatusOK},
		{name: "no content", status: http.StatusNoContent},
		{name: "last success code", status: 299},
		{name: "redirect", status: http.StatusMultipleChoices, wantError: true},
		{name: "bad request", status: http.StatusBadRequest, wantError: true},
		{name: "server error", status: http.StatusInternalServerError, wantError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := &auditResponseBody{Reader: strings.NewReader("")}
			client := auditHTTPClientFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tt.status, Body: body}, nil
			})
			observer, err := NewHTTPObserver("http://localhost:9090/audit", client)
			require.NoError(t, err)

			err = observer.Update(context.Background(), Event{Metrics: []string{"Alloc"}})

			if tt.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.True(t, body.closed)
		})
	}
}

// Сохраняем сетевые ошибки и таймаут для последующего логирования сервером.
func TestHTTPObserverUpdateReturnsClientError(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
	}{
		{name: "connection error", err: errors.New("connection refused")},
		{name: "timeout", err: context.DeadlineExceeded},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := auditHTTPClientFunc(func(req *http.Request) (*http.Response, error) {
				return nil, tt.err
			})
			observer, err := NewHTTPObserver("http://localhost:9090/audit", client)
			require.NoError(t, err)

			err = observer.Update(context.Background(), Event{Metrics: []string{"Alloc"}})

			require.ErrorIs(t, err, tt.err)
		})
	}
}

// Отмена входящего запроса должна отменить и отправку аудита.
func TestHTTPObserverUpdatePassesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := auditHTTPClientFunc(func(req *http.Request) (*http.Response, error) {
		cancel()
		require.ErrorIs(t, req.Context().Err(), context.Canceled)
		return nil, req.Context().Err()
	})
	observer, err := NewHTTPObserver("http://localhost:9090/audit", client)
	require.NoError(t, err)

	err = observer.Update(ctx, Event{Metrics: []string{"Alloc"}})

	require.ErrorIs(t, err, context.Canceled)
}

// Ошибка чтения ответа возвращается с освобождением тела ответа.
func TestHTTPObserverUpdateReturnsReadError(t *testing.T) {
	body := &auditResponseBody{Reader: iotest.ErrReader(io.ErrUnexpectedEOF)}
	client := auditHTTPClientFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
	})
	observer, err := NewHTTPObserver("http://localhost:9090/audit", client)
	require.NoError(t, err)

	err = observer.Update(context.Background(), Event{Metrics: []string{"Alloc"}})

	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	require.True(t, body.closed)
}

type auditHTTPClientFunc func(*http.Request) (*http.Response, error)

// Подменяет сетевую отправку проверкой запроса в памяти.
func (f auditHTTPClientFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

type auditResponseBody struct {
	io.Reader
	closed bool
}

// Позволяет проверить освобождение тела ответа без сетевого соединения.
func (b *auditResponseBody) Close() error {
	b.closed = true
	return nil
}
