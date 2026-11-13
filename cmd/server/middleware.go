package main

import (
	"crypto/rsa"
	"net/http"

	handlermiddleware "github.com/IvanSaratov/go-metrics-practice/internal/handler/middleware"
	"go.uber.org/zap"
)

// Запрос проходит проверку подписи, расшифровку, затем распаковку gzip.
// Ответ подписываем после сжатия.
func withMiddleware(
	next http.Handler,
	appLogger *zap.Logger,
	key string,
	privateKey *rsa.PrivateKey,
) http.Handler {
	return handlermiddleware.LoggingMiddleware(appLogger)(
		handlermiddleware.SignatureMiddleware(key)(
			handlermiddleware.CryptoMiddleware(privateKey)(
				handlermiddleware.GzipMiddleware(next),
			),
		),
	)
}
