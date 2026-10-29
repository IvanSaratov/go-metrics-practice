package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/IvanSaratov/go-metrics-practice/internal/audit"
	"github.com/IvanSaratov/go-metrics-practice/internal/handler"
	"go.uber.org/zap"
)

const (
	auditQueueSize       = 64
	auditShutdownTimeout = 5 * time.Second
)

// Создаёт отдельную очередь и фоновый обработчик для каждого приёмника.
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
	var closer auditCloser
	addObserver := func(name string, observer audit.Observer) {
		async := audit.NewAsyncObserver(observer, auditQueueSize, func(err error) {
			appLogger.Error("failed to deliver audit events", zap.String("observer", name), zap.Error(err))
		})
		observers = append(observers, async)
		closer = append(closer, async)
	}
	if config.auditFile != "" {
		file, err := openFile(config.auditFile)
		if err != nil {
			return nil, nil, fmt.Errorf("open audit file: %w", err)
		}
		addObserver("file", audit.NewFileObserver(file))
	}
	if httpObserver != nil {
		addObserver("http", httpObserver)
	}
	if len(observers) == 0 {
		return nil, nil, nil
	}

	publisher := audit.NewPublisher(observers...)
	return func(ctx context.Context, event audit.Event) {
		_ = publisher.Notify(ctx, event)
	}, closer, nil
}

type auditCloser []*audit.AsyncObserver

// Закрытие всех наблюдателей параллельно в пределах одного таймаута.
func (observers auditCloser) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), auditShutdownTimeout)
	defer cancel()
	errs := make([]error, len(observers))
	var wg sync.WaitGroup
	for i, observer := range observers {
		wg.Go(func() {
			errs[i] = observer.Shutdown(ctx)
		})
	}
	wg.Wait()
	return errors.Join(errs...)
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
