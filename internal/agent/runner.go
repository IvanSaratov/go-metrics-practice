package agent

import (
	"context"
	"time"

	log "github.com/sirupsen/logrus"
)

type Agent struct {
	metrics        *Metrics
	client         *Client
	pollInterval   time.Duration
	reportInterval time.Duration
}

func NewAgent(metrics *Metrics, client *Client, pollInterval time.Duration, reportInterval time.Duration) *Agent {
	return &Agent{
		metrics:        metrics,
		client:         client,
		pollInterval:   pollInterval,
		reportInterval: reportInterval,
	}
}

func (a *Agent) Run(ctx context.Context) {
	pollTicker := time.NewTicker(a.pollInterval)
	defer pollTicker.Stop()

	reportTicker := time.NewTicker(a.reportInterval)
	defer reportTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-pollTicker.C:
			a.metrics.collectRuntimeMetrics()
		case <-reportTicker.C:
			if err := a.client.SendBatch(a.metrics.snapshot()); err != nil {
				log.WithError(err).Warn("failed to report metrics")
			}
		}
	}
}
