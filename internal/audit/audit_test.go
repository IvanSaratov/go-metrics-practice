package audit

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// Проверяем формат события, который будут получать оба приёмника аудита.
func TestEventMarshalJSON(t *testing.T) {
	event := Event{
		Timestamp: 12345678,
		Metrics:   []string{"Alloc", "Frees"},
		IPAddress: "192.168.0.42",
	}

	body, err := json.Marshal(event)

	require.NoError(t, err)
	require.JSONEq(t, `{"ts":12345678,"metrics":["Alloc","Frees"],"ip_address":"192.168.0.42"}`, string(body))
}

// Проверяем доставку всем наблюдателям и сохранение ошибок каждого приёмника.
func TestPublisherNotify(t *testing.T) {
	firstErr := errors.New("first observer failed")
	secondErr := errors.New("second observer failed")
	tests := []struct {
		name           string
		observerErrors []error
		wantErrors     []error
	}{
		{
			name: "no observers",
		},
		{
			name:           "all observers succeed",
			observerErrors: []error{nil, nil},
		},
		{
			name:           "continue after observer failure",
			observerErrors: []error{firstErr, nil},
			wantErrors:     []error{firstErr},
		},
		{
			name:           "collect all observer failures",
			observerErrors: []error{firstErr, secondErr, nil},
			wantErrors:     []error{firstErr, secondErr},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			event := Event{
				Timestamp: 12345678,
				Metrics:   []string{"Alloc", "Frees"},
				IPAddress: "192.168.0.42",
			}
			receivedEvents := make([][]Event, len(tt.observerErrors))
			receivedContexts := make([]context.Context, len(tt.observerErrors))
			observers := make([]Observer, len(tt.observerErrors))
			for index, observerErr := range tt.observerErrors {
				observers[index] = observerFunc(func(ctx context.Context, event Event) error {
					receivedEvents[index] = append(receivedEvents[index], event)
					receivedContexts[index] = ctx
					return observerErr
				})
			}
			publisher := NewPublisher(observers...)

			err := publisher.Notify(ctx, event)

			if len(tt.wantErrors) == 0 {
				require.NoError(t, err)
			}
			for _, wantErr := range tt.wantErrors {
				require.ErrorIs(t, err, wantErr)
			}
			for index := range observers {
				require.Equal(t, []Event{event}, receivedEvents[index])
				require.Same(t, ctx, receivedContexts[index])
			}
		})
	}
}

type observerFunc func(context.Context, Event) error

// Update позволяет использовать тестовую функцию как наблюдателя.
func (f observerFunc) Update(ctx context.Context, event Event) error {
	return f(ctx, event)
}
