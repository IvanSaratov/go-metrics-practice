package agent

import (
	"context"
	"time"

	log "github.com/sirupsen/logrus"
)

type Agent struct {
	metrics        *Metrics
	sender         MetricsSender
	pollInterval   time.Duration
	reportInterval time.Duration
}

func NewAgent(metrics *Metrics, sender MetricsSender, pollInterval time.Duration, reportInterval time.Duration) *Agent {
	return &Agent{
		metrics:        metrics,
		sender:         sender,
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
			a.poll()
		case <-reportTicker.C:
			if err := a.report(); err != nil {
				log.WithError(err).Warn("failed to report metrics")
			}
		}
	}
}

func (a *Agent) poll() {
	PollRuntimeMetrics(a.metrics)
}

func (a *Agent) report() error {
	return ReportMetrics(a.metrics.snapshot(), a.sender)
}
