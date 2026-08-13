package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/IvanSaratov/go-metrics-practice/internal/config/db"
	"github.com/IvanSaratov/go-metrics-practice/internal/handler"
	handlermiddleware "github.com/IvanSaratov/go-metrics-practice/internal/handler/middleware"
	applogger "github.com/IvanSaratov/go-metrics-practice/internal/logger"
	"github.com/IvanSaratov/go-metrics-practice/internal/repository"
	"github.com/urfave/cli/v2"
	"go.uber.org/zap"
)

type serverConfig struct {
	address         string
	storeInterval   time.Duration
	fileStoragePath string
	restore         bool
	databaseDSN     string
}

func main() {
	// Инициализируем наш логер
	appLogger, err := applogger.New(false)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "create logger: %v\n", err)
		os.Exit(1)
	}

	// Запуска программу
	exitCode := run(os.Args, appLogger)
	// Ошибка Sync не должна менять код завершения приложения
	_ = appLogger.Sync()
	os.Exit(exitCode)
}

func run(args []string, appLogger *zap.Logger) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Вот так вот сложно - что бы тестировать
	app := newServerApp(func(config serverConfig) error {
		return runServer(ctx, config, appLogger)
	})

	if err := app.Run(args); err != nil {
		appLogger.Error("server failed", zap.Error(err))
		return 1
	}

	return 0
}

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
		})
	}

	return app
}

// Выносим обхявление всех middleware в отдельную функцию для переопределения последовательности
func newServerHandler(storage repository.Storage, appLogger *zap.Logger) http.Handler {
	router := handler.NewRouter(storage)
	// Логгер считает размер уже сжатого ответа
	return handlermiddleware.LoggingMiddleware(appLogger)(
		handlermiddleware.GzipMiddleware(router),
	)
}

func runServer(
	ctx context.Context,
	config serverConfig,
	appLogger *zap.Logger,
) (resultErr error) {
	storage := repository.NewFileStorage(
		config.fileStoragePath,
		config.storeInterval == 0,
	)
	if config.restore {
		if err := storage.Restore(); err != nil {
			return fmt.Errorf("restore metrics: %w", err)
		}
	}

	database, err := db.Open(config.databaseDSN)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() {
		if err := database.Close(); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("close database: %w", err))
		}
	}()

	listener, err := net.Listen("tcp", config.address)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", config.address, err)
	}
	defer listener.Close()

	// Запускаем наш таймер с сохранением
	stopPeriodicSave := startPeriodicSave(config.storeInterval, storage, appLogger)
	defer func() {
		stopPeriodicSave()
		if err := storage.Save(); err != nil {
			resultErr = errors.Join(
				resultErr,
				fmt.Errorf("save metrics on shutdown: %w", err),
			)
		}
	}()

	server := &http.Server{
		Addr:    config.address,
		Handler: newServerHandler(storage, appLogger),
	}

	// Буфер позволяет Serve завершиться, пока выполняется остановка сервера
	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- server.Serve(listener)
	}()

	appLogger.Info("server started", zap.String("address", listener.Addr().String()))

	select {
	case err := <-serveErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", err)
		}
		appLogger.Info("server stopped")
		return nil
	case <-ctx.Done():
		appLogger.Info("shutdown requested")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	shutdownErr := server.Shutdown(shutdownCtx)
	// Ожидаем Serve, чтобы не потерять ошибку и не оставить горутину
	serveErr := <-serveErrors

	if shutdownErr != nil {
		return fmt.Errorf("shutdown server: %w", shutdownErr)
	}
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return fmt.Errorf("serve HTTP during shutdown: %w", serveErr)
	}

	appLogger.Info("server stopped")
	return nil
}
