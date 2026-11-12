package main

import (
	"context"
	"fmt"
	"time"

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
	app := &cli.Command{Name: "server"}
	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:    "address",
			Aliases: []string{"a"},
			Sources: cli.EnvVars("ADDRESS"),
			Value:   "localhost:8080",
			Usage:   "HTTP server address",
		},
		&cli.Int64Flag{
			Name:    "store-interval",
			Aliases: []string{"i"},
			Sources: cli.EnvVars("STORE_INTERVAL"),
			Value:   300,
			Usage:   "metrics storage interval in seconds",
		},
		&cli.StringFlag{
			Name:    "file-storage-path",
			Aliases: []string{"f"},
			Sources: cli.EnvVars("FILE_STORAGE_PATH"),
			Value:   "./temp/metrics-db.json",
			Usage:   "metrics storage file path",
		},
		&cli.BoolFlag{
			Name:    "restore",
			Aliases: []string{"r"},
			Sources: cli.EnvVars("RESTORE"),
			Value:   true,
			Usage:   "restore metrics from the storage file",
		},
		&cli.StringFlag{
			Name:    "database-dsn",
			Aliases: []string{"d"},
			Sources: cli.EnvVars("DATABASE_DSN"),
			Usage:   "PostgreSQL connection string",
		},
		&cli.StringFlag{
			Name:    "key",
			Aliases: []string{"k"},
			Sources: cli.EnvVars("KEY"),
			Usage:   "Key for signing HTTP bodies",
		},
		&cli.StringFlag{
			Name:    "crypto-key",
			Sources: cli.EnvVars("CRYPTO_KEY"),
			Usage:   "Путь к файлу с приватным ключом шифрования",
		},
		&cli.StringFlag{
			Name:    "audit-file",
			Sources: cli.EnvVars("AUDIT_FILE"),
			Usage:   "Audit log file path",
		},
		&cli.StringFlag{
			Name:    "audit-url",
			Sources: cli.EnvVars("AUDIT_URL"),
			Usage:   "Audit log receiver URL",
		},
	}
	app.Action = func(_ context.Context, cmd *cli.Command) error {
		storeInterval := cmd.Int64("store-interval")
		if storeInterval < 0 {
			return fmt.Errorf("store interval must not be negative")
		}

		return run(serverConfig{
			address:         cmd.String("address"),
			storeInterval:   time.Duration(storeInterval) * time.Second,
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
