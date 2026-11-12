package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/IvanSaratov/go-metrics-practice/internal/agent"
	"github.com/IvanSaratov/go-metrics-practice/internal/buildinfo"
	"github.com/IvanSaratov/go-metrics-practice/internal/cliflags"
	"github.com/IvanSaratov/go-metrics-practice/internal/cryptoutil"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v3"
)

var (
	buildVersion string
	buildDate    string
	buildCommit  string
)

type agentConfig struct {
	serverAddress  string
	pollInterval   time.Duration
	reportInterval time.Duration
	timeout        time.Duration
	key            string
	cryptoKeyPath  string
	rateLimit      int
}

func main() {
	buildinfo.Print(os.Stdout, buildVersion, buildDate, buildCommit)
	runMain()
}

// Анализатор запрещает os.Exit в main, поэтому выходим здесь.
// Сначала run завершится и выполнит свои defer, затем вызовется os.Exit.
func runMain() {
	os.Exit(run(os.Args))
}

func run(args []string) int {
	app := newAgentApp(runAgent)
	if err := app.Run(context.Background(), args); err != nil {
		log.Error(err)
		return 1
	}
	return 0
}

func newAgentApp(run func(config agentConfig) error) *cli.Command {
	app := &cli.Command{Name: "agent"}
	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:    "a",
			Aliases: []string{"address"},
			Sources: cli.EnvVars("ADDRESS"),
			Value:   "localhost:8080",
			Usage:   "HTTP server address to connect",
		},
		&cli.GenericFlag{
			Name:    "p",
			Aliases: []string{"poll_interval"},
			Sources: cli.EnvVars("POLL_INTERVAL"),
			Value:   cliflags.NewDuration(2 * time.Second),
			Usage:   "Set runtime metrics poll interval",
		},
		&cli.GenericFlag{
			Name:    "r",
			Aliases: []string{"report_interval"},
			Sources: cli.EnvVars("REPORT_INTERVAL"),
			Value:   cliflags.NewDuration(10 * time.Second),
			Usage:   "Set metrics report interval",
		},
		&cli.GenericFlag{
			Name:    "t",
			Aliases: []string{"timeout"},
			Sources: cli.EnvVars("TIMEOUT"),
			Value:   cliflags.NewDuration(30 * time.Second),
			Usage:   "Server connection timeout",
		},
		&cli.StringFlag{
			Name:    "key",
			Aliases: []string{"k"},
			Sources: cli.EnvVars("KEY"),
			Usage:   "Key for signing request bodies",
		},
		&cli.StringFlag{
			Name:    "crypto-key",
			Sources: cli.EnvVars("CRYPTO_KEY"),
			Usage:   "Путь к файлу с публичным ключом шифрования",
		},
		&cli.IntFlag{
			Name:    "l",
			Aliases: []string{"rate-limit"},
			Sources: cli.EnvVars("RATE_LIMIT"),
			Value:   1,
			Usage:   "Maximum number of concurrent requests",
		},
	}
	app.Action = func(_ context.Context, cmd *cli.Command) error {
		rateLimit := cmd.Int("l")
		// Количество будущих воркеров должно быть положительным.
		if rateLimit <= 0 {
			return fmt.Errorf("rate limit must be positive")
		}

		return run(agentConfig{
			serverAddress:  cmd.String("a"),
			pollInterval:   cmd.Generic("p").(*cliflags.Duration).Duration(),
			reportInterval: cmd.Generic("r").(*cliflags.Duration).Duration(),
			timeout:        cmd.Generic("t").(*cliflags.Duration).Duration(),
			key:            cmd.String("key"),
			cryptoKeyPath:  cmd.String("crypto-key"),
			rateLimit:      rateLimit,
		})
	}

	return app
}

func runAgent(config agentConfig) error {
	// Читаем ключ один раз, до запуска воркеров. Пустой путь отключает шифрование.
	publicKey, err := cryptoutil.LoadPublicKey(config.cryptoKeyPath)
	if err != nil {
		return fmt.Errorf("load public key: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	httpClient := &http.Client{
		Timeout: config.timeout,
	}
	client := agent.NewClient(
		cliflags.NormalizeBaseURL(config.serverAddress),
		httpClient,
		config.key,
		publicKey,
	)
	metrics := agent.NewMetrics()
	metricsAgent := agent.NewAgent(
		metrics,
		client,
		config.pollInterval,
		config.reportInterval,
		config.rateLimit,
	)

	log.Info("starting agent")
	metricsAgent.Run(ctx)
	log.Info("agent stopped")
	return nil
}
