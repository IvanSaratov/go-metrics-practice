package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/IvanSaratov/go-metrics-practice/internal/agent"
	log "github.com/sirupsen/logrus"
)

const (
	serverAddress  = "http://localhost:8080"
	pollInterval   = 2 * time.Second
	reportInterval = 10 * time.Second
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	httpClient := &http.Client{
		Timeout: 3 * time.Second,
	}
	client := agent.NewClient(serverAddress, httpClient)
	metrics := agent.NewMetrics()
	metricsAgent := agent.NewAgent(metrics, client, pollInterval, reportInterval)

	log.Info("starting agent")
	metricsAgent.Run(ctx)
	log.Info("agent stopped")
}
