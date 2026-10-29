package main

import (
	"fmt"
	"time"

	"github.com/urfave/cli/v2"
)

type serverConfig struct {
	address         string
	storeInterval   time.Duration
	fileStoragePath string
	restore         bool
	databaseDSN     string
	key             string
	auditFile       string
	auditURL        string
}

// Собирает параметры запуска и передаёт настройки серверу.
func newServerApp(run func(config serverConfig) error) *cli.App {
	app := cli.NewApp()
	app.Name = "server"
	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:    "address",
			Aliases: []string{"a"},
			EnvVars: []string{"ADDRESS"},
			Value:   "localhost:8080",
			Usage:   "HTTP server address",
		},
		&cli.Int64Flag{
			Name:    "store-interval",
			Aliases: []string{"i"},
			EnvVars: []string{"STORE_INTERVAL"},
			Value:   300,
			Usage:   "metrics storage interval in seconds",
		},
		&cli.StringFlag{
			Name:    "file-storage-path",
			Aliases: []string{"f"},
			EnvVars: []string{"FILE_STORAGE_PATH"},
			Value:   "./temp/metrics-db.json",
			Usage:   "metrics storage file path",
		},
		&cli.BoolFlag{
			Name:    "restore",
			Aliases: []string{"r"},
			EnvVars: []string{"RESTORE"},
			Value:   true,
			Usage:   "restore metrics from the storage file",
		},
		&cli.StringFlag{
			Name:    "database-dsn",
			Aliases: []string{"d"},
			EnvVars: []string{"DATABASE_DSN"},
			Usage:   "PostgreSQL connection string",
		},
		&cli.StringFlag{
			Name:    "key",
			Aliases: []string{"k"},
			EnvVars: []string{"KEY"},
			Usage:   "Key for signing HTTP bodies",
		},
		&cli.StringFlag{
			Name:    "audit-file",
			EnvVars: []string{"AUDIT_FILE"},
			Usage:   "Audit log file path",
		},
		&cli.StringFlag{
			Name:    "audit-url",
			EnvVars: []string{"AUDIT_URL"},
			Usage:   "Audit log receiver URL",
		},
	}
	app.Action = func(ctx *cli.Context) error {
		storeInterval := ctx.Int64("store-interval")
		if storeInterval < 0 {
			return fmt.Errorf("store interval must not be negative")
		}

		return run(serverConfig{
			address:         ctx.String("address"),
			storeInterval:   time.Duration(storeInterval) * time.Second,
			fileStoragePath: ctx.String("file-storage-path"),
			restore:         ctx.Bool("restore"),
			databaseDSN:     ctx.String("database-dsn"),
			key:             ctx.String("key"),
			auditFile:       ctx.String("audit-file"),
			auditURL:        ctx.String("audit-url"),
		})
	}

	return app
}
