package middleware

import (
	"net/http"
	"time"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

func LoggingMiddleware(log *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			startedAt := time.Now()
			response := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(response, r)

			status := response.Status()
			if status == 0 {
				// Подставляем статус, который неявно использовал бы net/http.
				status = http.StatusOK
			}

			log.Info(
				"request completed",
				zap.String("uri", r.RequestURI),
				zap.String("method", r.Method),
				zap.Duration("duration", time.Since(startedAt)),
				zap.Int("status", status),
				zap.Int("size", response.BytesWritten()),
			)
		})
	}
}
