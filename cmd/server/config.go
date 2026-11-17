package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/IvanSaratov/go-metrics-practice/internal/cliflags"
	"github.com/IvanSaratov/go-metrics-practice/internal/config"
	altsrc "github.com/urfave/cli-altsrc/v3"
	altsrcjson "github.com/urfave/cli-altsrc/v3/json"
	"github.com/urfave/cli/v3"
)

type serverConfig struct {
	address         string
	shutdownTimeout time.Duration
	storeInterval   time.Duration
	fileStoragePath string
	restore         bool
	databaseDSN     string
	key             string
	cryptoKeyPath   string
	trustedSubnet   *net.IPNet
	auditFile       string
	auditURL        string
}

// Собирает параметры запуска и передаёт настройки серверу.
func newServerApp(run func(config serverConfig) error) *cli.Command {
	var configPath string
	jsonSource := altsrc.NewStringPtrSourcer(&configPath)
	app := &cli.Command{Name: "server"}
	app.Flags = []cli.Flag{
		// CONFIG должен заполнить путь до чтения JSON-источников остальных флагов.
		&cli.StringFlag{
			Name:        "config",
			Aliases:     []string{"c"},
			Sources:     cli.EnvVars("CONFIG"),
			Destination: &configPath,
			Usage:       "Путь к JSON-файлу конфигурации",
			Action: func(_ context.Context, _ *cli.Command, path string) error {
				return config.ValidateJSON(path)
			},
		},
		&cli.StringFlag{
			Name:    "address",
			Aliases: []string{"a"},
			Sources: cli.NewValueSourceChain(
				cli.EnvVar("ADDRESS"),
				altsrcjson.JSON("address", jsonSource),
			),
			Value: "localhost:8080",
			Usage: "HTTP server address",
		},
		&cli.GenericFlag{
			Name: "shutdown-timeout",
			Sources: cli.NewValueSourceChain(
				cli.EnvVar("SHUTDOWN_TIMEOUT"),
				altsrcjson.JSON("shutdown_timeout", jsonSource),
			),
			Value: cliflags.NewDuration(5 * time.Second),
			Usage: "Время ожидания активных HTTP-запросов при остановке",
		},
		&cli.GenericFlag{
			Name:    "store-interval",
			Aliases: []string{"i"},
			Sources: cli.NewValueSourceChain(
				cli.EnvVar("STORE_INTERVAL"),
				altsrcjson.JSON("store_interval", jsonSource),
			),
			Value: cliflags.NewDuration(300 * time.Second),
			Usage: "Интервал сохранения: длительность или целое число секунд",
		},
		&cli.StringFlag{
			Name:    "file-storage-path",
			Aliases: []string{"f"},
			Sources: cli.NewValueSourceChain(
				cli.EnvVar("FILE_STORAGE_PATH"),
				cli.EnvVar("STORE_FILE"),
				altsrcjson.JSON("store_file", jsonSource),
			),
			Value: "./temp/metrics-db.json",
			Usage: "metrics storage file path",
		},
		&cli.BoolFlag{
			Name:    "restore",
			Aliases: []string{"r"},
			Sources: cli.NewValueSourceChain(
				cli.EnvVar("RESTORE"),
				altsrcjson.JSON("restore", jsonSource),
			),
			Value: true,
			Usage: "restore metrics from the storage file",
		},
		&cli.StringFlag{
			Name:    "database-dsn",
			Aliases: []string{"d"},
			Sources: cli.NewValueSourceChain(
				cli.EnvVar("DATABASE_DSN"),
				altsrcjson.JSON("database_dsn", jsonSource),
			),
			Usage: "PostgreSQL connection string",
		},
		&cli.StringFlag{
			Name:    "key",
			Aliases: []string{"k"},
			Sources: cli.NewValueSourceChain(
				cli.EnvVar("KEY"),
				altsrcjson.JSON("key", jsonSource),
			),
			Usage: "Key for signing HTTP bodies",
		},
		&cli.StringFlag{
			Name: "crypto-key",
			Sources: cli.NewValueSourceChain(
				cli.EnvVar("CRYPTO_KEY"),
				altsrcjson.JSON("crypto_key", jsonSource),
			),
			Usage: "Путь к файлу с приватным ключом шифрования",
		},
		&cli.StringFlag{
			Name:    "trusted-subnet",
			Aliases: []string{"t"},
			Sources: cli.EnvVars("TRUSTED_SUBNET"),
			Usage:   "Доверенная подсеть агентов в формате CIDR",
		},
		&cli.StringFlag{
			Name: "audit-file",
			Sources: cli.NewValueSourceChain(
				cli.EnvVar("AUDIT_FILE"),
				altsrcjson.JSON("audit_file", jsonSource),
			),
			Usage: "Audit log file path",
		},
		&cli.StringFlag{
			Name: "audit-url",
			Sources: cli.NewValueSourceChain(
				cli.EnvVar("AUDIT_URL"),
				altsrcjson.JSON("audit_url", jsonSource),
			),
			Usage: "Audit log receiver URL",
		},
	}
	app.Action = func(_ context.Context, cmd *cli.Command) error {
		shutdownTimeout := cmd.Generic("shutdown-timeout").(*cliflags.Duration).Duration()
		if shutdownTimeout <= 0 {
			return fmt.Errorf("shutdown timeout must be positive")
		}
		storeInterval := cmd.Generic("store-interval").(*cliflags.Duration).Duration()
		if storeInterval < 0 {
			return fmt.Errorf("store interval must not be negative")
		}

		value := cmd.String("trusted-subnet")
		if !cmd.IsSet("trusted-subnet") {
			// altsrc превращает [] в пустую строку. Читаем JSON сами, если его не перекрыли флагом или окружением.
			var err error
			value, err = readTrustedSubnetJSON(configPath)
			if err != nil {
				return err
			}
		}
		// Разбираем CIDR один раз при запуске. Пустая строка оставляет доступ без ограничений.
		var trustedSubnet *net.IPNet
		if value != "" {
			var err error
			_, trustedSubnet, err = net.ParseCIDR(value)
			if err != nil {
				return fmt.Errorf("invalid trusted subnet %q: %w", value, err)
			}
		}

		return run(serverConfig{
			address:         cmd.String("address"),
			shutdownTimeout: shutdownTimeout,
			storeInterval:   storeInterval,
			fileStoragePath: cmd.String("file-storage-path"),
			restore:         cmd.Bool("restore"),
			databaseDSN:     cmd.String("database-dsn"),
			key:             cmd.String("key"),
			cryptoKeyPath:   cmd.String("crypto-key"),
			trustedSubnet:   trustedSubnet,
			auditFile:       cmd.String("audit-file"),
			auditURL:        cmd.String("audit-url"),
		})
	}

	return app
}

func readTrustedSubnetJSON(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read config %q: %w", path, err)
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(data, &values); err != nil {
		return "", fmt.Errorf("decode config %q: %w", path, err)
	}
	value, exists := values["trusted_subnet"]
	if !exists {
		return "", nil
	}
	var subnet *string
	if err := json.Unmarshal(value, &subnet); err != nil || subnet == nil {
		return "", fmt.Errorf("invalid trusted subnet in config %q: trusted_subnet must be a string", path)
	}
	return *subnet, nil
}
