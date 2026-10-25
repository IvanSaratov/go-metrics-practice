package audit

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAsyncObserverDoesNotBlockOtherObservers(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	slow := NewAsyncObserver(observerFunc(func(context.Context, Event) error {
		close(started)
		<-release
		return nil
	}), 1, nil)
	fastDone := make(chan struct{})
	var received Event
	fast := NewAsyncObserver(observerFunc(func(_ context.Context, event Event) error {
		received = event
		close(fastDone)
		return nil
	}), 1, nil)
	t.Cleanup(func() {
		stopAsyncObserver(t, slow)
		stopAsyncObserver(t, fast)
	})

	event := Event{Timestamp: 123, Metrics: []string{"Alloc"}, IPAddress: "127.0.0.1"}
	publisher := NewPublisher(slow, fast)
	notified := make(chan struct{})
	var notifyErr error
	go func() {
		notifyErr = publisher.Notify(context.Background(), event)
		close(notified)
	}()

	waitForAudit(t, started)
	waitForAudit(t, notified)
	waitForAudit(t, fastDone)
	require.NoError(t, notifyErr)
	require.Equal(t, event, received)
}

func TestAsyncObserverDropsNewestWhenQueueIsFull(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	var received []int64
	var reported []error
	observer := NewAsyncObserver(observerFunc(func(_ context.Context, event Event) error {
		if event.Timestamp == 1 {
			close(started)
			<-release
		}
		received = append(received, event.Timestamp)
		return nil
	}), 1, func(err error) { reported = append(reported, err) })
	t.Cleanup(func() { stopAsyncObserver(t, observer) })
	require.NoError(t, observer.Update(context.Background(), Event{Timestamp: 1}))
	waitForAudit(t, started)

	notified := make(chan struct{})
	go func() {
		_ = observer.Update(context.Background(), Event{Timestamp: 2})
		_ = observer.Update(context.Background(), Event{Timestamp: 3})
		close(notified)
	}()
	waitForAudit(t, notified)
	require.Equal(t, uint64(1), observer.Dropped())
	require.Empty(t, reported, "потери должны сообщаться фоновой горутиной")

	unblock()
	stopAsyncObserver(t, observer)
	require.Equal(t, []int64{1, 2}, received)
	require.Len(t, reported, 1)
	require.EqualError(t, reported[0], "dropped 1 audit events")
}

func TestAsyncObserverCopiesQueuedEvent(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	var received Event
	observer := NewAsyncObserver(observerFunc(func(_ context.Context, event Event) error {
		if event.Timestamp == 1 {
			close(started)
			<-release
		} else {
			received = event
		}
		return nil
	}), 1, nil)
	t.Cleanup(func() { stopAsyncObserver(t, observer) })
	require.NoError(t, observer.Update(context.Background(), Event{Timestamp: 1}))
	waitForAudit(t, started)

	event := Event{Timestamp: 2, Metrics: []string{"Alloc"}}
	require.NoError(t, observer.Update(context.Background(), event))
	event.Metrics[0] = "changed"
	unblock()
	stopAsyncObserver(t, observer)
	require.Equal(t, []string{"Alloc"}, received.Metrics)
}

func TestAsyncObserverDoesNotUseRequestContext(t *testing.T) {
	delivered := make(chan struct{})
	var deliveryErr error
	observer := NewAsyncObserver(observerFunc(func(ctx context.Context, _ Event) error {
		deliveryErr = ctx.Err()
		close(delivered)
		return deliveryErr
	}), 1, nil)
	t.Cleanup(func() { stopAsyncObserver(t, observer) })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.NoError(t, observer.Update(ctx, Event{}))
	waitForAudit(t, delivered)
	require.NoError(t, deliveryErr)
}

func TestAsyncObserverReportsErrorsAndContinues(t *testing.T) {
	wantErr := errors.New("audit unavailable")
	var received []int64
	var reported []error
	observer := NewAsyncObserver(observerFunc(func(_ context.Context, event Event) error {
		received = append(received, event.Timestamp)
		if event.Timestamp == 1 {
			return wantErr
		}
		return nil
	}), 2, func(err error) { reported = append(reported, err) })
	t.Cleanup(func() { stopAsyncObserver(t, observer) })

	require.NoError(t, observer.Update(context.Background(), Event{Timestamp: 1}))
	require.NoError(t, observer.Update(context.Background(), Event{Timestamp: 2}))
	stopAsyncObserver(t, observer)
	require.Equal(t, []int64{1, 2}, received)
	require.Len(t, reported, 1)
	require.ErrorIs(t, reported[0], wantErr)
}

func TestAsyncObserverShutdownDrainsQueueAndClosesFile(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "audit-*.jsonl")
	require.NoError(t, err)
	t.Cleanup(func() { _ = file.Close() })
	observer := NewAsyncObserver(NewFileObserver(file), 2, nil)
	t.Cleanup(func() { stopAsyncObserver(t, observer) })
	events := []Event{{Timestamp: 1, Metrics: []string{"Alloc"}}, {Timestamp: 2, Metrics: []string{"PollCount"}}}
	for _, event := range events {
		require.NoError(t, observer.Update(context.Background(), event))
	}

	stopAsyncObserver(t, observer)
	body, err := os.ReadFile(file.Name())
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	require.Len(t, lines, 2)
	for index, line := range lines {
		var event Event
		require.NoError(t, json.Unmarshal([]byte(line), &event))
		require.Equal(t, events[index], event)
	}
	_, err = file.WriteString("unexpected write")
	require.ErrorIs(t, err, os.ErrClosed)
}

func TestAsyncObserverShutdownCancelsPendingDelivery(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	var received []int64
	var deliveryErr error
	var reported []error
	observer := NewAsyncObserver(observerFunc(func(ctx context.Context, event Event) error {
		received = append(received, event.Timestamp)
		if event.Timestamp == 1 {
			close(started)
			select {
			case <-ctx.Done():
				deliveryErr = ctx.Err()
			case <-release:
			}
		}
		return deliveryErr
	}), 1, func(err error) { reported = append(reported, err) })
	t.Cleanup(func() { stopAsyncObserver(t, observer) })
	require.NoError(t, observer.Update(context.Background(), Event{Timestamp: 1}))
	waitForAudit(t, started)
	require.NoError(t, observer.Update(context.Background(), Event{Timestamp: 2}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	stopped := make(chan struct{})
	var shutdownErr error
	go func() {
		shutdownErr = observer.Shutdown(ctx)
		close(stopped)
	}()
	waitForAudit(t, stopped)
	require.ErrorIs(t, shutdownErr, context.Canceled)
	stopAsyncObserver(t, observer)
	require.ErrorIs(t, deliveryErr, context.Canceled)
	require.Equal(t, []int64{1}, received)
	require.Equal(t, uint64(1), observer.Dropped())
	require.Len(t, reported, 2)
	require.ErrorIs(t, reported[0], context.Canceled)
	require.EqualError(t, reported[1], "dropped 1 audit events")
}

func TestAsyncObserverConcurrentUpdatesAndShutdown(t *testing.T) {
	var delivered uint64
	observer := NewAsyncObserver(observerFunc(func(context.Context, Event) error {
		delivered++
		return nil
	}), 4, nil)
	t.Cleanup(func() { stopAsyncObserver(t, observer) })
	start := make(chan struct{})
	var workers sync.WaitGroup
	for range 10 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			for range 20 {
				_ = observer.Update(context.Background(), Event{})
			}
		}()
	}
	close(start)
	stopAsyncObserver(t, observer)
	workers.Wait()
	require.Equal(t, uint64(200), delivered+observer.Dropped())
}

func waitForAudit(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("операция аудита не завершилась")
	}
}

func stopAsyncObserver(t *testing.T, observer *AsyncObserver) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, observer.Shutdown(ctx))
}
