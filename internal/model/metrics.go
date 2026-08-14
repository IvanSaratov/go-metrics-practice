package models

import "fmt"

const (
	Counter = "counter"
	Gauge   = "gauge"
)

type Metrics struct {
	ID    string   `json:"id"`
	MType string   `json:"type"`
	Delta *int64   `json:"delta,omitempty"`
	Value *float64 `json:"value,omitempty"`
	Hash  string   `json:"hash,omitempty"`
}

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

func ValidateUpdates(metrics []Metrics) error {
	for _, metric := range metrics {
		if err := metric.ValidateUpdate(); err != nil {
			return err
		}
	}

	return nil
}
