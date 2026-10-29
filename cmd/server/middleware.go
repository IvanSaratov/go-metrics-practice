package main

import (
	"net/http"

	handlermiddleware "github.com/IvanSaratov/go-metrics-practice/internal/handler/middleware"
	"go.uber.org/zap"
)

// Проверяем подпись до распаковки запроса, а ответ подписываем после сжатия.
func withMiddleware(
	next http.Handler,
	appLogger *zap.Logger,
	key string,
) http.Handler {
	return handlermiddleware.LoggingMiddleware(appLogger)(
		handlermiddleware.SignatureMiddleware(key)(
			handlermiddleware.GzipMiddleware(next),
		),
	)
}
