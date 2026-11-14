package main

import (
	"crypto/rsa"
	"net/http"

	handlermiddleware "github.com/IvanSaratov/go-metrics-practice/internal/handler/middleware"
	"go.uber.org/zap"
)

// После проверки подписи расшифровываем запрос при наличии ключа, затем распаковываем gzip.
// Ответ подписываем после сжатия.
func withMiddleware(
	next http.Handler,
	appLogger *zap.Logger,
	key string,
	privateKey *rsa.PrivateKey,
) http.Handler {
	next = handlermiddleware.GzipMiddleware(next)
	if privateKey != nil {
		next = handlermiddleware.CryptoMiddleware(privateKey)(next)
	}
	next = handlermiddleware.SignatureMiddleware(key)(next)
	return handlermiddleware.LoggingMiddleware(appLogger)(next)
}
