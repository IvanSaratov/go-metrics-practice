package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/IvanSaratov/go-metrics-practice/internal/audit"
	"github.com/stretchr/testify/require"
)

// Файл нельзя закрывать, пока запрос ещё пишет аудит.
func TestDrainingHandlerWaitsForAudit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		file := &serverAuditFile{}
		fileObserver := audit.NewFileObserver(file)
		event := audit.Event{Timestamp: 12345678, Metrics: []string{"Alloc"}, IPAddress: "192.168.0.42"}
		started := make(chan struct{})
		release := make(chan struct{})
		releaseRequest := sync.OnceFunc(func() { close(release) })
		defer releaseRequest()
		written := make(chan error, 1)
		requests := &drainingHandler{next: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(started)
			<-release
			written <- fileObserver.Update(r.Context(), event)
			w.WriteHeader(http.StatusOK)
		})}
		response := httptest.NewRecorder()
		go requests.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/update", nil))
		<-started

		closed := make(chan error, 1)
		go func() {
			requests.wait()
			closed <- fileObserver.Close()
		}()
		synctest.Wait()
		require.False(t, file.closed, "audit file closed before the active request completed")

		releaseRequest()
		require.NoError(t, <-written)
		require.NoError(t, <-closed)
		require.True(t, file.closed)
		require.Equal(t, http.StatusOK, response.Code)
		var got audit.Event
		require.NoError(t, json.Unmarshal(file.Bytes(), &got))
		require.Equal(t, event, got)

		lateResponse := httptest.NewRecorder()
		requests.ServeHTTP(lateResponse, httptest.NewRequest(http.MethodPost, "/update", nil))
		require.Equal(t, http.StatusServiceUnavailable, lateResponse.Code)
	})
}
