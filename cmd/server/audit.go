package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/IvanSaratov/go-metrics-practice/internal/audit"
	"github.com/IvanSaratov/go-metrics-practice/internal/handler"
	"go.uber.org/zap"
)

// Создаёт приёмники аудита и уведомление с логированием ошибок доставки.
func newAudit(
	config serverConfig,
	appLogger *zap.Logger,
	openFile func(string) (io.WriteCloser, error),
	client audit.HTTPClient,
) (handler.AuditNotifier, io.Closer, error) {
	var httpObserver *audit.HTTPObserver
	if config.auditURL != "" {
		var err error
		httpObserver, err = audit.NewHTTPObserver(config.auditURL, client)
		if err != nil {
			return nil, nil, err
		}
	}

	var observers []audit.Observer
	var closer io.Closer
	if config.auditFile != "" {
		file, err := openFile(config.auditFile)
		if err != nil {
			return nil, nil, fmt.Errorf("open audit file: %w", err)
		}
		fileObserver := audit.NewFileObserver(file)
		observers = append(observers, fileObserver)
		closer = fileObserver
	}
	if httpObserver != nil {
		observers = append(observers, httpObserver)
	}
	if len(observers) == 0 {
		return nil, nil, nil
	}

	publisher := audit.NewPublisher(observers...)
	return func(ctx context.Context, event audit.Event) {
		if err := publisher.Notify(ctx, event); err != nil {
			appLogger.Error("failed to audit request", zap.Error(err))
		}
	}, closer, nil
}

// Открывает журнал для дописывания, сохраняя существующие записи.
func openAuditFile(path string) (io.WriteCloser, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
}

// Сохраняет исходный POST и адрес приёмника при ответе с перенаправлением.
func newAuditHTTPClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
