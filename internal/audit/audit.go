// Package audit передаёт события обновления метрик в файл или на HTTP-сервер.
package audit

import (
	"context"
	"errors"
)

// Event описывает успешный запрос на обновление метрик.
type Event struct {
	// Timestamp — Unix-время события в секундах.
	Timestamp int64 `json:"ts"`
	// Metrics содержит имена обновлённых метрик.
	Metrics []string `json:"metrics"`
	// IPAddress — IP-адрес клиента без порта.
	IPAddress string `json:"ip_address"`
}

// Observer принимает события аудита.
type Observer interface {
	// Update обрабатывает одно событие.
	Update(context.Context, Event) error
}

// Publisher передаёт события всем приёмникам аудита.
type Publisher struct {
	observers []Observer
}

// NewPublisher создаёт издателя с заданными приёмниками аудита.
func NewPublisher(observers ...Observer) *Publisher {
	return &Publisher{observers: append([]Observer(nil), observers...)}
}

// Notify по очереди передаёт событие всем приёмникам.
// Ошибки отдельных приёмников не прерывают отправку и возвращаются вместе.
func (p *Publisher) Notify(ctx context.Context, event Event) error {
	var notificationErrors []error
	for _, observer := range p.observers {
		if err := observer.Update(ctx, event); err != nil {
			notificationErrors = append(notificationErrors, err)
		}
	}
	return errors.Join(notificationErrors...)
}
