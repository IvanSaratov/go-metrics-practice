package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/IvanSaratov/go-metrics-practice/internal/audit"
	"github.com/IvanSaratov/go-metrics-practice/internal/repository"
	"github.com/stretchr/testify/require"
)

// Дожидаемся записи метрик и аудита перед сохранением и закрытием файлов.
func TestDrainingHandlerWaitsForActiveRequest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		storagePath := filepath.Join(t.TempDir(), "metrics.json")
		storage := repository.NewFileStorage(storagePath, false)
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
			written <- errors.Join(
				storage.SetGauge(r.Context(), "Alloc", 67.1),
				fileObserver.Update(r.Context(), event),
			)
			w.WriteHeader(http.StatusOK)
		})}
		response := httptest.NewRecorder()
		go requests.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/update", nil))
		<-started

		closed := make(chan error, 1)
		go func() {
			requests.wait()
			closed <- errors.Join(storage.Save(), fileObserver.Close())
		}()
		synctest.Wait()
		require.False(t, file.closed, "audit file closed before the active request completed")
		require.NoFileExists(t, storagePath, "metrics saved before the active request completed")

		releaseRequest()
		require.NoError(t, <-written)
		require.NoError(t, <-closed)
		require.True(t, file.closed)
		require.Equal(t, http.StatusOK, response.Code)
		var got audit.Event
		require.NoError(t, json.Unmarshal(file.Bytes(), &got))
		require.Equal(t, event, got)
		restored := repository.NewFileStorage(storagePath, false)
		require.NoError(t, restored.Restore())
		value, ok, err := restored.GetGauge(context.Background(), "Alloc")
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, 67.1, value)

		lateResponse := httptest.NewRecorder()
		requests.ServeHTTP(lateResponse, httptest.NewRequest(http.MethodPost, "/update", nil))
		require.Equal(t, http.StatusServiceUnavailable, lateResponse.Code)
	})
}
