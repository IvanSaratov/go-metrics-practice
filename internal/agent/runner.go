// Package agent собирает метрики приложения и системы и отправляет их на сервер.
package agent

import (
	"context"
	"fmt"
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
// Перед возвратом ждёт текущие операции и отправляет оставшиеся метрики.
// Обычные ошибки пишет в лог, ошибку финальной отправки возвращает вызывающему коду.
func (a *Agent) Run(ctx context.Context) error {
	// Сигнал останавливает расписание, но не должен прерывать начатые запросы.
	workCtx := context.WithoutCancel(ctx)
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
			a.reportWorker(workCtx, reports)
		}()
	}

	workers.Wait()
	if err := a.report(workCtx); err != nil {
		return fmt.Errorf("send final metrics: %w", err)
	}
	return nil
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
			if ctx.Err() != nil {
				return
			}
			a.metrics.collectRuntimeMetrics()
		}
	}
}

// периодически обновляет системные метрики
func (a *Agent) collectSystem(ctx context.Context) {
	ticker := time.NewTicker(a.pollInterval)
	defer ticker.Stop()
	// Уже начатый сбор должен закончиться до финальной отправки.
	workCtx := context.WithoutCancel(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if ctx.Err() != nil {
				return
			}
			values, err := a.systemCollector.collect(workCtx)
			if err != nil {
				log.WithError(err).Warn("failed to collect system metrics")
				continue
			}
			a.metrics.updateSystemMetrics(values)
		}
	}
}

// передаёт задания свободным воркерам без накопления очереди
func (a *Agent) scheduleReports(ctx context.Context, reports chan<- struct{}) {
	// Закрытый канал сообщает воркерам, что новых заданий больше не будет.
	defer close(reports)
	ticker := time.NewTicker(a.reportInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if ctx.Err() != nil {
				return
			}
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
	for range reports {
		if err := a.report(ctx); err != nil {
			log.WithError(err).Warn("failed to report metrics")
		}
	}
}

// При ошибке возвращает счётчики в накопитель для следующей отправки.
func (a *Agent) report(ctx context.Context) error {
	batch := a.metrics.takeSnapshot()
	if err := a.client.SendBatch(ctx, batch); err != nil {
		a.metrics.restoreCounters(batch)
		return err
	}
	return nil
}
