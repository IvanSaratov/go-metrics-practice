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

	"github.com/IvanSaratov/go-metrics-practice/internal/handler"
	handlermiddleware "github.com/IvanSaratov/go-metrics-practice/internal/handler/middleware"
	applogger "github.com/IvanSaratov/go-metrics-practice/internal/logger"
	"github.com/IvanSaratov/go-metrics-practice/internal/repository"
	"github.com/urfave/cli/v2"
	"go.uber.org/zap"
)

type serverConfig struct {
	address string
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
	// Ошибка Sync не должна менять код завершения приложения.
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
			Name:    "a",
			Aliases: []string{"address"},
			EnvVars: []string{"ADDRESS"},
			Value:   "localhost:8080",
			Usage:   "HTTP server address",
		},
	}
	app.Action = func(ctx *cli.Context) error {
		return run(serverConfig{
			address: ctx.String("a"),
		})
	}

	return app
}

// Выносим обхявление всех middleware в отдельную функцию для переопределения последовательности
func newServerHandler(storage repository.Storage, appLogger *zap.Logger) http.Handler {
	router := handler.NewRouter(storage)
	// Логгер считает размер уже сжатого ответа.
	return handlermiddleware.LoggingMiddleware(appLogger)(
		handlermiddleware.GzipMiddleware(router),
	)
}

func runServer(ctx context.Context, config serverConfig, appLogger *zap.Logger) error {
	listener, err := net.Listen("tcp", config.address)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", config.address, err)
	}
	defer listener.Close()

	storage := repository.NewMemStorage()

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
