package main

import (
	"context"
	"encoding/json"
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
	altsrc "github.com/urfave/cli-altsrc/v3"
	altsrcjson "github.com/urfave/cli-altsrc/v3/json"
	"github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"
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

// validateJSONConfig проверяет файл, поскольку altsrc скрывает ошибки чтения.
func validateJSONConfig(path string) error {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config %q: %w", path, err)
	}
	if !json.Valid(data) {
		return fmt.Errorf("config %q contains invalid JSON", path)
	}

	// JSON-источник altsrc использует YAML-парсер. Проверяем, что он примет объект.
	var config map[any]any
	if err := yaml.Unmarshal(data, &config); err != nil || config == nil {
		return fmt.Errorf("config %q must contain a JSON object without duplicate keys", path)
	}
	return nil
}

func newAgentApp(run func(config agentConfig) error) *cli.Command {
	var configPath string
	jsonSource := altsrc.NewStringPtrSourcer(&configPath)
	app := &cli.Command{Name: "agent"}
	app.Flags = []cli.Flag{
		// CONFIG должен заполнить путь до чтения JSON-источников остальных флагов.
		&cli.StringFlag{
			Name:        "config",
			Aliases:     []string{"c"},
			Sources:     cli.EnvVars("CONFIG"),
			Destination: &configPath,
			Usage:       "Путь к JSON-файлу конфигурации",
			Action: func(_ context.Context, _ *cli.Command, path string) error {
				return validateJSONConfig(path)
			},
		},
		&cli.StringFlag{
			Name:    "a",
			Aliases: []string{"address"},
			Sources: cli.NewValueSourceChain(
				cli.EnvVar("ADDRESS"),
				altsrcjson.JSON("address", jsonSource),
			),
			Value: "localhost:8080",
			Usage: "HTTP server address to connect",
		},
		&cli.GenericFlag{
			Name:    "p",
			Aliases: []string{"poll_interval"},
			Sources: cli.NewValueSourceChain(
				cli.EnvVar("POLL_INTERVAL"),
				altsrcjson.JSON("poll_interval", jsonSource),
			),
			Value: cliflags.NewDuration(2 * time.Second),
			Usage: "Set runtime metrics poll interval",
		},
		&cli.GenericFlag{
			Name:    "r",
			Aliases: []string{"report_interval"},
			Sources: cli.NewValueSourceChain(
				cli.EnvVar("REPORT_INTERVAL"),
				altsrcjson.JSON("report_interval", jsonSource),
			),
			Value: cliflags.NewDuration(10 * time.Second),
			Usage: "Set metrics report interval",
		},
		&cli.GenericFlag{
			Name:    "t",
			Aliases: []string{"timeout"},
			Sources: cli.NewValueSourceChain(
				cli.EnvVar("TIMEOUT"),
				altsrcjson.JSON("timeout", jsonSource),
			),
			Value: cliflags.NewDuration(30 * time.Second),
			Usage: "Server connection timeout",
		},
		&cli.StringFlag{
			Name:    "key",
			Aliases: []string{"k"},
			Sources: cli.NewValueSourceChain(
				cli.EnvVar("KEY"),
				altsrcjson.JSON("key", jsonSource),
			),
			Usage: "Key for signing request bodies",
		},
		&cli.StringFlag{
			Name: "crypto-key",
			Sources: cli.NewValueSourceChain(
				cli.EnvVar("CRYPTO_KEY"),
				altsrcjson.JSON("crypto_key", jsonSource),
			),
			Usage: "Путь к файлу с публичным ключом шифрования",
		},
		&cli.IntFlag{
			Name:    "l",
			Aliases: []string{"rate-limit"},
			Sources: cli.NewValueSourceChain(
				cli.EnvVar("RATE_LIMIT"),
				altsrcjson.JSON("rate_limit", jsonSource),
			),
			Value: 1,
			Usage: "Maximum number of concurrent requests",
		},
	}
	app.Action = func(_ context.Context, cmd *cli.Command) error {
		rateLimit := cmd.Int("l")
		// Количество будущих воркеров должно быть положительным.
		if rateLimit <= 0 {
			return fmt.Errorf("rate limit must be positive")
		}
		pollInterval := cmd.Generic("p").(*cliflags.Duration).Duration()
		reportInterval := cmd.Generic("r").(*cliflags.Duration).Duration()
		// Тикеры агента не принимают нулевые и отрицательные интервалы.
		if pollInterval <= 0 {
			return fmt.Errorf("poll interval must be positive")
		}
		if reportInterval <= 0 {
			return fmt.Errorf("report interval must be positive")
		}

		return run(agentConfig{
			serverAddress:  cmd.String("a"),
			pollInterval:   pollInterval,
			reportInterval: reportInterval,
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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT)
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
	if err := metricsAgent.Run(ctx); err != nil {
		return err
	}
	log.Info("agent stopped")
	return nil
}
