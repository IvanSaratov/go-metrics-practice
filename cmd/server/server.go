package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/IvanSaratov/go-metrics-practice/internal/config/db"
	"github.com/IvanSaratov/go-metrics-practice/internal/handler"
	"github.com/IvanSaratov/go-metrics-practice/internal/repository"
	"go.uber.org/zap"
)

// Запускает сервер с нужным хранилищем и аудитом.
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

	storage, err := newStorage(config, database)
	if err != nil {
		return err
	}

	notifyAudit, auditCloser, err := newAudit(config, appLogger, openAuditFile, newAuditHTTPClient())
	if err != nil {
		return err
	}
	if auditCloser != nil {
		defer func() {
			if err := auditCloser.Close(); err != nil {
				appLogger.Error("failed to stop audit", zap.Error(err))
			}
		}()
	}

	// Не кладём nil *sql.DB в интерфейс: он тогда не будет равен nil.
	var handlerDatabase handler.Database
	if database != nil {
		handlerDatabase = database
	}
	router := handler.NewServer(storage, handlerDatabase, notifyAudit)
	listener, err := net.Listen("tcp", config.address)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", config.address, err)
	}
	defer listener.Close()

	if fileStorage, ok := storage.(*repository.FileStorage); ok {
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

	requests := &drainingHandler{next: withMiddleware(router, appLogger, config.key)}
	server := &http.Server{
		Addr:    config.address,
		Handler: requests,
	}
	defer func() {
		// При таймауте Shutdown закрываем соединения и ждём обработчиков.
		if err := server.Close(); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("close HTTP server: %w", err))
		}
		requests.wait()
	}()

	// Чтобы Serve не ждал, пока мы закончим Shutdown.
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
	// Не теряем ошибку Serve, если остановка уже началась.
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
