package main

import (
	"context"
	"fmt"
	"time"

	"github.com/IvanSaratov/go-metrics-practice/internal/cliflags"
	altsrc "github.com/urfave/cli-altsrc/v3"
	altsrcjson "github.com/urfave/cli-altsrc/v3/json"
	"github.com/urfave/cli/v3"
)

type serverConfig struct {
	address         string
	storeInterval   time.Duration
	fileStoragePath string
	restore         bool
	databaseDSN     string
	key             string
	cryptoKeyPath   string
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
				return validateJSONConfig(path)
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
		storeInterval := cmd.Generic("store-interval").(*cliflags.Duration).Duration()
		if storeInterval < 0 {
			return fmt.Errorf("store interval must not be negative")
		}

		return run(serverConfig{
			address:         cmd.String("address"),
			storeInterval:   storeInterval,
			fileStoragePath: cmd.String("file-storage-path"),
			restore:         cmd.Bool("restore"),
			databaseDSN:     cmd.String("database-dsn"),
			key:             cmd.String("key"),
			cryptoKeyPath:   cmd.String("crypto-key"),
			auditFile:       cmd.String("audit-file"),
			auditURL:        cmd.String("audit-url"),
		})
	}

	return app
}
