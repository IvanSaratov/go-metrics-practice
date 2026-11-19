// Package agent собирает метрики приложения и системы и отправляет их на сервер.
package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/IvanSaratov/go-metrics-practice/internal/model"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

// BatchSender отправляет снимок метрик по выбранному протоколу.
type BatchSender interface {
	SendBatch(context.Context, []model.Metrics) error
}

// Agent периодически собирает метрики и отправляет их батчами.
type Agent struct {
	metrics         *Metrics
	client          BatchSender
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
	client BatchSender,
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

// Run собирает и отправляет метрики до отмены ctx или ошибки создания запроса.
// При отмене ctx ждёт текущие операции и отправляет оставшиеся метрики.
func (a *Agent) Run(ctx context.Context) error {
	workers, runCtx := errgroup.WithContext(ctx)
	// Начатые запросы и финальная отправка должны завершиться даже после сигнала.
	workCtx := context.WithoutCancel(ctx)
	reports := make(chan struct{})

	workers.Go(func() error {
		a.collectRuntime(runCtx)
		return nil
	})
	workers.Go(func() error {
		a.collectSystem(runCtx)
		return nil
	})
	workers.Go(func() error {
		a.scheduleReports(runCtx, reports)
		return nil
	})
	for range a.rateLimit {
		workers.Go(func() error {
			return a.reportWorker(workCtx, reports)
		})
	}

	if err := workers.Wait(); err != nil {
		return err
	}
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
func (a *Agent) reportWorker(ctx context.Context, reports <-chan struct{}) error {
	for range reports {
		if err := a.report(ctx); err != nil {
			// Адрес задан при запуске, поэтому повтор с тем же адресом не поможет.
			if errors.Is(err, errCreateRequest) {
				return err
			}
			log.WithError(err).Warn("failed to report metrics")
		}
	}
	return nil
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
