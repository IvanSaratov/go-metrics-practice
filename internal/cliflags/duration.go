// Package cliflags помогает читать параметры командной строки.
package cliflags

import (
	"fmt"
	"strconv"
	"time"
)

// Duration хранит длительность из флага командной строки.
type Duration struct {
	value time.Duration
}

// NewDuration создаёт значение флага с начальной длительностью value.
func NewDuration(value time.Duration) *Duration {
	return &Duration{value: value}
}

// Set принимает длительность вроде "500ms" или целое число секунд.
// При ошибке текущее значение не меняется.
func (d *Duration) Set(value string) error {
	duration, err := time.ParseDuration(value)
	if err == nil {
		d.value = duration
		return nil
	}

	seconds, secondsErr := strconv.ParseInt(value, 10, 64)
	if secondsErr != nil {
		return fmt.Errorf("invalid duration %q", value)
	}

	d.value = time.Duration(seconds) * time.Second
	return nil
}

// String возвращает длительность строкой, например "2s".
func (d *Duration) String() string {
	return d.value.String()
}

// Duration возвращает сохранённую длительность.
func (d *Duration) Duration() time.Duration {
	return d.value
}
