package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/IvanSaratov/go-metrics-practice/internal/config/db"
	"github.com/IvanSaratov/go-metrics-practice/internal/cryptoutil"
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
	// Проверяем ключ до открытия БД и HTTP-порта.
	privateKey, err := cryptoutil.LoadPrivateKey(config.cryptoKeyPath)
	if err != nil {
		return fmt.Errorf("load private key: %w", err)
	}

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

	server := &http.Server{
		Addr:    config.address,
		Handler: withMiddleware(router, appLogger, config.key, privateKey),
	}

	serveDone := make(chan struct{})
	var serveErr error
	go func() {
		serveErr = server.Serve(listener)
		close(serveDone)
	}()

	appLogger.Info("server started", zap.String("address", listener.Addr().String()))

	select {
	case <-serveDone:
	case <-ctx.Done():
		appLogger.Info("shutdown requested")
	}

	// Завершаем активные запросы и при сигнале, и при ошибке слушателя.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), config.shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		resultErr = fmt.Errorf("shutdown server: %w", err)
		if closeErr := server.Close(); closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("close HTTP server: %w", closeErr))
		}
	}
	<-serveDone
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		resultErr = errors.Join(resultErr, fmt.Errorf("serve HTTP: %w", serveErr))
	}
	if resultErr == nil {
		appLogger.Info("server stopped")
	}
	return resultErr
}
