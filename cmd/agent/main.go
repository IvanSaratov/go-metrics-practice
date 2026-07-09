package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/IvanSaratov/go-metrics-practice/internal/agent"
	"github.com/IvanSaratov/go-metrics-practice/internal/helpers"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

type agentConfig struct {
	serverAddress  string
	pollInterval   time.Duration
	reportInterval time.Duration
}

func main() {
	app := newAgentApp(runAgent)
	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func newAgentApp(run func(config agentConfig) error) *cli.App {
	app := cli.NewApp()
	app.Name = "agent"
	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:  "a",
			Value: "localhost:8080",
			Usage: "HTTP server address",
		},
		&cli.DurationFlag{
			Name:  "p",
			Value: 2 * time.Second,
			Usage: "runtime metrics poll interval",
		},
		&cli.DurationFlag{
			Name:  "r",
			Value: 10 * time.Second,
			Usage: "metrics report interval",
		},
	}
	app.Action = func(ctx *cli.Context) error {
		return run(agentConfig{
			serverAddress:  ctx.String("a"),
			pollInterval:   ctx.Duration("p"),
			reportInterval: ctx.Duration("r"),
		})
	}

	return app
}

func runAgent(config agentConfig) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	httpClient := &http.Client{
		Timeout: 3 * time.Second,
	}
	client := agent.NewClient(helpers.NormalizeBaseURL(config.serverAddress), httpClient)
	metrics := agent.NewMetrics()
	metricsAgent := agent.NewAgent(metrics, client, config.pollInterval, config.reportInterval)

	log.Info("starting agent")
	metricsAgent.Run(ctx)
	log.Info("agent stopped")
	return nil
}
