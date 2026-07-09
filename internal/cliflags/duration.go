package cliflags

import (
	"fmt"
	"strconv"
	"time"
)

type Duration struct {
	value time.Duration
}

func NewDuration(value time.Duration) *Duration {
	return &Duration{value: value}
}

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

func (d *Duration) String() string {
	return d.value.String()
}

func (d *Duration) Duration() time.Duration {
	return d.value
}
