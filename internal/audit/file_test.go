package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Проверяем сохранение старых записей и отдельную строку для каждого события.
func TestFileObserverUpdateAppendsJSONLines(t *testing.T) {
	file := &auditFileStub{}
	existing := `{ "ts":1, "metrics":["old"], "ip_address":"127.0.0.1" }`
	file.Buffer.WriteString(existing + "\n")
	observer := NewFileObserver(file)

	events := []Event{
		{
			Timestamp: 12345678,
			Metrics:   []string{"Alloc", "Frees"},
			IPAddress: "192.168.0.42",
		},
		{
			Timestamp: 12345679,
			Metrics:   []string{"NumGC"},
			IPAddress: "192.168.0.43",
		},
	}
	for _, event := range events {
		require.NoError(t, observer.Update(context.Background(), event))
	}

	lines := strings.Split(file.String(), "\n")
	require.Len(t, lines, 4)
	require.Equal(t, existing, lines[0])
	require.Empty(t, lines[3])
	received := make([]Event, len(events))
	for index, line := range lines[1:3] {
		require.NoError(t, json.Unmarshal([]byte(line), &received[index]))
	}
	require.Equal(t, events, received)
}

// Ошибка записи должна дойти до издателя для последующего логирования.
func TestFileObserverUpdateReturnsWriteError(t *testing.T) {
	wantErr := errors.New("disk unavailable")
	file := &auditFileStub{writeErr: wantErr}
	observer := NewFileObserver(file)

	err := observer.Update(context.Background(), Event{Metrics: []string{"Alloc"}})

	require.ErrorIs(t, err, wantErr)
}

// Одновременные запросы должны сохранять все события без смешивания строк.
func TestFileObserverConcurrentUpdates(t *testing.T) {
	file := &auditFileStub{}
	observer := NewFileObserver(file)
	const count = 64
	events := make([]Event, count)
	results := make(chan error, count)
	for index := range events {
		events[index] = Event{
			Timestamp: int64(index + 1),
			Metrics:   []string{"Alloc"},
			IPAddress: "192.168.0.42",
		}
		go func(event Event) {
			results <- observer.Update(context.Background(), event)
		}(events[index])
	}
	for range count {
		require.NoError(t, <-results)
	}

	lines := strings.Split(file.String(), "\n")
	require.Len(t, lines, count+1)
	require.Empty(t, lines[count])
	received := make([]Event, count)
	for index, line := range lines[:count] {
		require.NoError(t, json.Unmarshal([]byte(line), &received[index]))
	}
	require.ElementsMatch(t, events, received)
}

// Проверяем освобождение файла и возврат ошибки закрытия.
func TestFileObserverClose(t *testing.T) {
	wantErr := errors.New("close failed")
	for _, tt := range []struct {
		name     string
		closeErr error
	}{
		{name: "success"},
		{name: "failure", closeErr: wantErr},
	} {
		t.Run(tt.name, func(t *testing.T) {
			file := &auditFileStub{closeErr: tt.closeErr}
			observer := NewFileObserver(file)

			err := observer.Close()

			require.True(t, file.closed)
			if tt.closeErr == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tt.closeErr)
			}
		})
	}
}

type auditFileStub struct {
	bytes.Buffer
	writeErr error
	closeErr error
	closed   bool
}

// Имитирует файловую запись без обращения к диску.
func (f *auditFileStub) Write(data []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return f.Buffer.Write(data)
}

// Отмечает закрытие файла и возвращает заданную ошибку.
func (f *auditFileStub) Close() error {
	f.closed = true
	return f.closeErr
}
