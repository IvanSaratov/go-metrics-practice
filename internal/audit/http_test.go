package audit

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"testing/iotest"
	"testing/synctest"
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
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := &auditResponseBody{Reader: strings.NewReader("")}
			attempts := 0
			client := auditHTTPClientFunc(func(req *http.Request) (*http.Response, error) {
				attempts++
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
			require.Equal(t, 1, attempts)
		})
	}
}

func TestHTTPObserverUpdateRetriesTemporaryFailures(t *testing.T) {
	for _, tt := range []struct {
		name    string
		err     error
		status  int
		readErr error
	}{
		{name: "connection error", err: &net.OpError{Op: "dial", Err: errors.New("connection refused")}},
		{name: "connection closed", err: &url.Error{Op: "Post", Err: io.EOF}},
		{name: "internal server error", status: http.StatusInternalServerError},
		{name: "unavailable", status: http.StatusServiceUnavailable},
		{name: "truncated server response", status: http.StatusServiceUnavailable, readErr: io.ErrUnexpectedEOF},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				event := Event{Timestamp: 123, Metrics: []string{"Alloc"}, IPAddress: "192.168.0.42"}
				started := time.Now()
				var attempts []time.Duration
				var responseBody *auditResponseBody
				client := auditHTTPClientFunc(func(req *http.Request) (*http.Response, error) {
					defer req.Body.Close()
					if responseBody != nil {
						require.True(t, responseBody.closed, "предыдущий ответ должен быть закрыт до повтора")
					}
					var received Event
					require.NoError(t, json.NewDecoder(req.Body).Decode(&received))
					require.Equal(t, event, received)
					require.Equal(t, http.MethodPost, req.Method)
					require.Equal(t, "application/json", req.Header.Get("Content-Type"))
					attempts = append(attempts, time.Since(started))
					status := http.StatusNoContent
					responseBody = &auditResponseBody{Reader: strings.NewReader("")}
					if len(attempts) < 3 {
						if tt.err != nil {
							responseBody = nil
							return nil, tt.err
						}
						status = tt.status
						if tt.readErr != nil {
							responseBody.Reader = iotest.ErrReader(tt.readErr)
						}
					}
					return &http.Response{StatusCode: status, Body: responseBody}, nil
				})
				observer, err := NewHTTPObserver("https://example.com/audit", client)
				require.NoError(t, err)

				err = observer.Update(context.Background(), event)

				require.NoError(t, err)
				require.Equal(t, []time.Duration{0, time.Second, 4 * time.Second}, attempts)
				require.True(t, responseBody.closed)
			})
		})
	}
}

func TestHTTPObserverUpdateRetriesWithinDeadline(t *testing.T) {
	for _, tt := range []struct {
		name     string
		timeout  time.Duration
		attempts int
	}{
		{name: "audit timeout", timeout: 5 * time.Second, attempts: 3},
		{name: "earlier parent deadline", timeout: 2 * time.Second, attempts: 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx := context.Background()
				if tt.timeout < 5*time.Second {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, tt.timeout)
					defer cancel()
				}
				attempts := 0
				client := auditHTTPClientFunc(func(req *http.Request) (*http.Response, error) {
					defer req.Body.Close()
					attempts++
					return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: http.NoBody}, nil
				})
				observer, err := NewHTTPObserver("https://example.com/audit", client)
				require.NoError(t, err)
				started := time.Now()

				err = observer.Update(ctx, Event{})

				require.ErrorIs(t, err, context.DeadlineExceeded)
				require.Equal(t, tt.timeout, time.Since(started))
				require.Equal(t, tt.attempts, attempts)
			})
		})
	}
}

func TestHTTPObserverUpdateCancelsRetryWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		attempts := 0
		client := auditHTTPClientFunc(func(req *http.Request) (*http.Response, error) {
			defer req.Body.Close()
			attempts++
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: http.NoBody}, nil
		})
		observer, err := NewHTTPObserver("https://example.com/audit", client)
		require.NoError(t, err)
		started := time.Now()
		result := make(chan error, 1)
		go func() { result <- observer.Update(ctx, Event{}) }()
		synctest.Wait()

		cancel()

		require.ErrorIs(t, <-result, context.Canceled)
		require.Equal(t, 1, attempts)
		require.Zero(t, time.Since(started))
	})
}

// Возвращаем ошибки клиента и таймаут для последующего логирования сервером.
func TestHTTPObserverUpdateReturnsClientError(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
	}{
		{name: "non-network error", err: errors.New("client failure")},
		{name: "timeout", err: context.DeadlineExceeded},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				client := auditHTTPClientFunc(func(req *http.Request) (*http.Response, error) {
					return nil, tt.err
				})
				observer, err := NewHTTPObserver("http://localhost:9090/audit", client)
				require.NoError(t, err)

				err = observer.Update(context.Background(), Event{Metrics: []string{"Alloc"}})

				require.ErrorIs(t, err, tt.err)
			})
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
