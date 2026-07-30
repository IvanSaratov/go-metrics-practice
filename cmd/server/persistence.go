package main

import (
	"context"
	"time"

	"go.uber.org/zap"
)

type metricsSaver interface {
	Save() error
}

// Вынесены отдельные функции переодической записи в файл в отдельный пакет - что бы не захламлять main
func saveMetricsPeriodically(
	ctx context.Context,
	ticks <-chan time.Time,
	storage metricsSaver,
	appLogger *zap.Logger,
) {
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-ticks:
			if !ok {
				return
			}
			if err := storage.Save(); err != nil {
				appLogger.Info("save metrics", zap.Error(err))
			}
		}
	}
}

func startPeriodicSave(
	interval time.Duration,
	storage metricsSaver,
	appLogger *zap.Logger,
) func() {
	if interval <= 0 {
		return func() {}
	}

	// Отдельный контекст позволяет дождаться остановки ticker перед финальным снимком
	ctx, cancel := context.WithCancel(context.Background())
	ticker := time.NewTicker(interval)
	done := make(chan struct{})

	go func() {
		defer close(done)
		defer ticker.Stop()
		saveMetricsPeriodically(ctx, ticker.C, storage, appLogger)
	}()

	return func() {
		cancel()
		<-done
	}
}
