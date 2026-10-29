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
type Metrics struct {
	ID    string   `json:"id"`
	MType string   `json:"type"`
	Delta *int64   `json:"delta,omitempty"`
	Value *float64 `json:"value,omitempty"`
	Hash  string   `json:"hash,omitempty"`
}

// ValidateUpdate проверяет имя и тип метрики, а также поля Value и Delta.
func (m Metrics) ValidateUpdate() error {
	if m.ID == "" {
		return fmt.Errorf("metric id is required")
	}

	switch m.MType {
	case Gauge:
		if m.Value == nil || m.Delta != nil {
			return fmt.Errorf("gauge requires value only")
		}
	case Counter:
		if m.Delta == nil || m.Value != nil {
			return fmt.Errorf("counter requires delta only")
		}
	default:
		return fmt.Errorf("unsupported metric type %q", m.MType)
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
