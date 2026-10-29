// Package agent собирает метрики приложения и системы и отправляет их на сервер.
package agent

import (
	"context"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// Agent периодически собирает метрики и отправляет их через Client.
type Agent struct {
	metrics         *Metrics
	client          *Client
	pollInterval    time.Duration
	reportInterval  time.Duration
	rateLimit       int
	systemCollector systemMetricsCollector
}

// NewAgent создаёт агента для сбора и отправки метрик.
// pollInterval задаёт интервал сбора, reportInterval — интервал отправки.
// rateLimit ограничивает число одновременных запросов к серверу.
// Интервалы и rateLimit должны быть положительными.
func NewAgent(
	metrics *Metrics,
	client *Client,
	pollInterval time.Duration,
	reportInterval time.Duration,
	rateLimit int,
) *Agent {
	return &Agent{
		metrics:         metrics,
		client:          client,
		pollInterval:    pollInterval,
		reportInterval:  reportInterval,
		rateLimit:       rateLimit,
		systemCollector: defaultSystemMetricsCollector,
	}
}

// Run запускает сбор и отправку метрик и работает до отмены ctx.
// Перед возвратом ждёт завершения всех горутин агента.
// Ошибки сбора и отправки записываются в лог и не останавливают агента.
func (a *Agent) Run(ctx context.Context) {
	reports := make(chan struct{})
	var workers sync.WaitGroup
	// Лимит относится только к отправке, ещё три горутины собирают и планируют
	workers.Add(a.rateLimit + 3)

	go func() {
		defer workers.Done()
		a.collectRuntime(ctx)
	}()
	go func() {
		defer workers.Done()
		a.collectSystem(ctx)
	}()
	go func() {
		defer workers.Done()
		a.scheduleReports(ctx, reports)
	}()
	for range a.rateLimit {
		go func() {
			defer workers.Done()
			a.reportWorker(ctx, reports)
		}()
	}

	workers.Wait()
}

// периодически обновляет метрики среды выполнения Go
func (a *Agent) collectRuntime(ctx context.Context) {
	ticker := time.NewTicker(a.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.metrics.collectRuntimeMetrics()
		}
	}
}

// периодически обновляет системные метрики
func (a *Agent) collectSystem(ctx context.Context) {
	ticker := time.NewTicker(a.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			values, err := a.systemCollector.collect(ctx)
			if err != nil {
				if ctx.Err() == nil {
					log.WithError(err).Warn("failed to collect system metrics")
				}
				continue
			}
			a.metrics.updateSystemMetrics(values)
		}
	}
}

// передаёт задания свободным воркерам без накопления очереди
func (a *Agent) scheduleReports(ctx context.Context, reports chan<- struct{}) {
	ticker := time.NewTicker(a.reportInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			select {
			case reports <- struct{}{}:
			case <-ctx.Done():
				return
			}
		}
	}
}

// отправляет актуальный снимок метрик по заданию планировщика
func (a *Agent) reportWorker(ctx context.Context, reports <-chan struct{}) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-reports:
			if err := a.client.SendBatch(ctx, a.metrics.snapshot()); err != nil {
				if ctx.Err() == nil {
					log.WithError(err).Warn("failed to report metrics")
				}
			}
		}
	}
}
