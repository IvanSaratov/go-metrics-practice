package audit

import (
	"context"
	"fmt"
	"io"
	"sync"
)

// AsyncObserver передаёт события приёмнику через очередь и отдельную горутину.
type AsyncObserver struct {
	observer Observer
	events   chan Event
	onError  func(error)
	cancel   context.CancelFunc
	done     chan struct{}
	closeErr error

	mu      sync.Mutex
	closed  bool
	dropped uint64
}

// NewAsyncObserver запускает фоновую обработку с очередью размера bufferSize.
// bufferSize должен быть положительным. Ошибки доставки и число потерь передаются в onError, если он задан.
// Если приёмник реализует io.Closer, обёртка закроет его при остановке.
func NewAsyncObserver(observer Observer, bufferSize int, onError func(error)) *AsyncObserver {
	ctx, cancel := context.WithCancel(context.Background())
	async := &AsyncObserver{
		observer: observer,
		events:   make(chan Event, bufferSize),
		onError:  onError,
		cancel:   cancel,
		done:     make(chan struct{}),
	}
	go async.run(ctx)
	return async
}

// Update добавляет событие в очередь без ожидания приёмника и возвращает nil.
// При переполнении или после остановки событие отбрасывается.
// Контекст запроса не используется для фоновой обработки.
func (o *AsyncObserver) Update(_ context.Context, event Event) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		o.dropped++
		return nil
	}

	// Вызывающий код может изменить исходный список после возврата из Update.
	event.Metrics = append([]string(nil), event.Metrics...)
	select {
	case o.events <- event:
	default:
		o.dropped++
	}
	return nil
}

// Dropped возвращает число событий, отброшенных без вызова приёмника.
func (o *AsyncObserver) Dropped() uint64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.dropped
}

// Shutdown прекращает приём событий и ждёт обработки очереди и закрытия приёмника.
// При отмене ctx возвращает его ошибку и отменяет фоновую отправку.
// Приёмник закрывается фоновой горутиной после завершения текущего Update.
func (o *AsyncObserver) Shutdown(ctx context.Context) error {
	o.mu.Lock()
	if !o.closed {
		o.closed = true
		close(o.events)
	}
	o.mu.Unlock()

	select {
	case <-o.done:
		return o.closeErr
	case <-ctx.Done():
		o.cancel()
		return ctx.Err()
	}
}

func (o *AsyncObserver) run(ctx context.Context) {
	defer close(o.done)
	defer o.cancel()
	var reported uint64
	reportDropped := func() {
		dropped := o.Dropped()
		if dropped > reported && o.onError != nil {
			o.onError(fmt.Errorf("dropped %d audit events", dropped-reported))
		}
		reported = dropped
	}
	for event := range o.events {
		if ctx.Err() != nil {
			o.mu.Lock()
			o.dropped++
			o.mu.Unlock()
			continue
		}
		if err := o.observer.Update(ctx, event); err != nil && o.onError != nil {
			o.onError(err)
		}
		reportDropped()
	}
	reportDropped()
	if closer, ok := o.observer.(io.Closer); ok {
		o.closeErr = closer.Close()
	}
}
