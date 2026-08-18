package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
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

// Выносим объявление всех middleware в отдельную функцию для определения последовательности.
func withMiddleware(
	next http.Handler,
	appLogger *zap.Logger,
) http.Handler {
	// Логгер считает размер уже сжатого ответа
	return handlermiddleware.LoggingMiddleware(appLogger)(
		handlermiddleware.GzipMiddleware(next),
	)
}

func newStorage(config serverConfig, database *sql.DB) (repository.Storage, error) {
	// Если не пустой dsn то возвращаем postgres
	if database != nil {
		return repository.NewPostgresStorage(database), nil
	}

	// Если пустой путь для файла - то хранилище в памяти
	path := strings.TrimSpace(config.fileStoragePath)
	if path == "" {
		return repository.NewMemStorage(), nil
	}

	// Иначе создаем харнилище в файле внутри дефолтной директории
	storage := repository.NewFileStorage(path, config.storeInterval == 0)
	if config.restore {
		if err := storage.Restore(); err != nil {
			return nil, fmt.Errorf("restore metrics: %w", err)
		}
	}

	return storage, nil
}

func runServer(
	ctx context.Context,
	config serverConfig,
	appLogger *zap.Logger,
) (resultErr error) {
	var database *sql.DB
	databaseDSN := strings.TrimSpace(config.databaseDSN)
	if databaseDSN != "" {
		var err error
		database, err = db.Open(databaseDSN)
		if err != nil {
			return fmt.Errorf("open database: %w", err)
		}
		defer func() {
			if err := database.Close(); err != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("close database: %w", err))
			}
		}()

		if err := db.Migrate(ctx, database); err != nil {
			return err
		}
	}

	// Ппосле попытки подключиться к БД пытаемся создаеть его хранилище
	storage, err := newStorage(config, database)
	if err != nil {
		return err
	}

	var handlerDatabase handler.Database
	if database != nil {
		handlerDatabase = database
	}
	router := handler.NewServer(storage, handlerDatabase)
	listener, err := net.Listen("tcp", config.address)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", config.address, err)
	}
	defer listener.Close()

	if fileStorage, ok := storage.(*repository.FileStorage); ok {
		// Запускаем таймер только для файлового хранилища
		stopPeriodicSave := startPeriodicSave(config.storeInterval, fileStorage, appLogger)
		defer func() {
			stopPeriodicSave()
			if err := fileStorage.Save(); err != nil {
				resultErr = errors.Join(
					resultErr,
					fmt.Errorf("save metrics on shutdown: %w", err),
				)
			}
		}()
	}

	server := &http.Server{
		Addr:    config.address,
		Handler: withMiddleware(router, appLogger),
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
