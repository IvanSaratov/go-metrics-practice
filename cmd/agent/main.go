package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/IvanSaratov/go-metrics-practice/internal/agent"
	"github.com/IvanSaratov/go-metrics-practice/internal/cliflags"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

type agentConfig struct {
	serverAddress  string
	pollInterval   time.Duration
	reportInterval time.Duration
	timeout        time.Duration
	key            string
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
			Name:    "a",
			Aliases: []string{"address"},
			EnvVars: []string{"ADDRESS"},
			Value:   "localhost:8080",
			Usage:   "HTTP server address to connect",
		},
		&cli.GenericFlag{
			Name:    "p",
			Aliases: []string{"poll_interval"},
			EnvVars: []string{"POLL_INTERVAL"},
			Value:   cliflags.NewDuration(2 * time.Second),
			Usage:   "Set runtime metrics poll interval",
		},
		&cli.GenericFlag{
			Name:    "r",
			Aliases: []string{"report_interval"},
			EnvVars: []string{"REPORT_INTERVAL"},
			Value:   cliflags.NewDuration(10 * time.Second),
			Usage:   "Set metrics report interval",
		},
		&cli.GenericFlag{
			Name:    "t",
			Aliases: []string{"timeout"},
			EnvVars: []string{"TIMEOUT"},
			Value:   cliflags.NewDuration(30 * time.Second),
			Usage:   "Server connection timeout",
		},
		&cli.StringFlag{
			Name:    "key",
			Aliases: []string{"k"},
			EnvVars: []string{"KEY"},
			Usage:   "Key for signing request bodies",
		},
	}
	app.Action = func(ctx *cli.Context) error {
		return run(agentConfig{
			serverAddress:  ctx.String("a"),
			pollInterval:   ctx.Generic("p").(*cliflags.Duration).Duration(),
			reportInterval: ctx.Generic("r").(*cliflags.Duration).Duration(),
			timeout:        ctx.Generic("t").(*cliflags.Duration).Duration(),
			key:            ctx.String("key"),
		})
	}

	return app
}

func runAgent(config agentConfig) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	httpClient := &http.Client{
		Timeout: config.timeout,
	}
	client := agent.NewClient(
		cliflags.NormalizeBaseURL(config.serverAddress),
		httpClient,
		config.key,
	)
	metrics := agent.NewMetrics()
	metricsAgent := agent.NewAgent(metrics, client, config.pollInterval, config.reportInterval)

	log.Info("starting agent")
	metricsAgent.Run(ctx)
	log.Info("agent stopped")
	return nil
}
