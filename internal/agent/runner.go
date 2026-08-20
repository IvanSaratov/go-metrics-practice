package agent

import (
	"context"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

type Agent struct {
	metrics        *Metrics
	client         *Client
	pollInterval   time.Duration
	reportInterval time.Duration
	rateLimit      int
}

func NewAgent(
	metrics *Metrics,
	client *Client,
	pollInterval time.Duration,
	reportInterval time.Duration,
	rateLimit int,
) *Agent {
	return &Agent{
		metrics:        metrics,
		client:         client,
		pollInterval:   pollInterval,
		reportInterval: reportInterval,
		rateLimit:      rateLimit,
	}
}

// запускает независимый сбор метрик и пул воркеров отправки
func (a *Agent) Run(ctx context.Context) {
	reports := make(chan struct{})
	var workers sync.WaitGroup
	// +2 так как rate limit устанавливаем только на отправку
	// Один должен собирать в рантайме, другой планирует следующую очередь
	workers.Add(a.rateLimit + 2)

	go func() {
		defer workers.Done()
		a.collectRuntime(ctx)
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
