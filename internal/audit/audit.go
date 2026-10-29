package audit

import (
	"context"
	"errors"
)

// Описывает успешный запрос на обновление метрик.
type Event struct {
	Timestamp int64    `json:"ts"`
	Metrics   []string `json:"metrics"`
	IPAddress string   `json:"ip_address"`
}

// Принимает события аудита.
type Observer interface {
	Update(context.Context, Event) error
}

// Уведомляет подключённых наблюдателей.
type Publisher struct {
	observers []Observer
}

// Подключает приёмники аудита к издателю.
func NewPublisher(observers ...Observer) *Publisher {
	return &Publisher{observers: append([]Observer(nil), observers...)}
}

// Доставляет событие всем наблюдателям и объединяет ошибки доставки.
func (p *Publisher) Notify(ctx context.Context, event Event) error {
	var notificationErrors []error
	for _, observer := range p.observers {
		if err := observer.Update(ctx, event); err != nil {
			notificationErrors = append(notificationErrors, err)
		}
	}
	return errors.Join(notificationErrors...)
}
