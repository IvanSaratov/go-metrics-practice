// Package models содержит структуру метрики и функции её проверки.
package models

import "fmt"

const (
	// Counter — тип счётчика: при обновлении значение прибавляется к текущему.
	Counter = "counter"
	// Gauge — тип метрики, у которой хранится последнее значение.
	Gauge = "gauge"
)

// Metrics хранит данные одной метрики.
// У gauge значение находится в Value, у counter — в Delta.
// nil означает, что значение не передали.
// generate:reset
type Metrics struct {
	ID    string   `json:"id"`
	MType string   `json:"type"`
	Delta *int64   `json:"delta,omitempty"`
	Value *float64 `json:"value,omitempty"`
	Hash  string   `json:"hash,omitempty"`
}

// ValidateUpdate проверяет имя и тип метрики, а также поля Value и Delta.
func (r Metrics) ValidateUpdate() error {
	if r.ID == "" {
		return fmt.Errorf("metric id is required")
	}

	switch r.MType {
	case Gauge:
		if r.Value == nil || r.Delta != nil {
			return fmt.Errorf("gauge requires value only")
		}
	case Counter:
		if r.Delta == nil || r.Value != nil {
			return fmt.Errorf("counter requires delta only")
		}
	default:
		return fmt.Errorf("unsupported metric type %q", r.MType)
	}

	return nil
}

// ValidateUpdates проверяет метрики по очереди и возвращает первую ошибку.
// Пустой список проходит проверку.
func ValidateUpdates(metrics []Metrics) error {
	for _, metric := range metrics {
		if err := metric.ValidateUpdate(); err != nil {
			return err
		}
	}

	return nil
}
