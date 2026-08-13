package repository

import "context"

type Storage interface {
	SetGauge(ctx context.Context, name string, value float64) error
	AddCounter(ctx context.Context, name string, value int64) (int64, error)
	GetGauge(ctx context.Context, name string) (float64, bool, error)
	GetCounter(ctx context.Context, name string) (int64, bool, error)
	GetAllGauges(ctx context.Context) (map[string]float64, error)
	GetAllCounters(ctx context.Context) (map[string]int64, error)
}
