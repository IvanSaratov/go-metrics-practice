// Package handler содержит HTTP-обработчики для работы с метриками.
package handler

import (
	"crypto/rsa"
	"net/http"

	"github.com/IvanSaratov/go-metrics-practice/internal/handler/middleware"
	"github.com/IvanSaratov/go-metrics-practice/internal/repository"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

// Config задаёт логирование, ключи и аудит HTTP-запросов.
type Config struct {
	Logger      *zap.Logger
	SigningKey  string
	PrivateKey  *rsa.PrivateKey
	NotifyAudit AuditNotifier
}

// Server направляет HTTP-запросы к обработчикам метрик и проверки базы.
type Server struct {
	router  *chi.Mux
	storage repository.Storage
	db      Database
}

// NewServer создаёт сервер и подключает маршруты.
// Если db равен nil, запрос /ping возвращает 500.
func NewServer(storage repository.Storage, db Database, config Config) *Server {
	server := &Server{
		router:  chi.NewRouter(),
		storage: storage,
		db:      db,
	}
	server.registerRoutes(config)
	return server
}

// ServeHTTP передаёт запрос обработчику по его методу и пути.
func (s *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	s.router.ServeHTTP(writer, request)
}

func (s *Server) registerRoutes(config Config) {
	if config.Logger != nil {
		s.router.Use(middleware.LoggingMiddleware(config.Logger))
	}
	// Проверяем подпись исходного тела до расшифровки и распаковки.
	s.router.Use(middleware.SignatureMiddleware(config.SigningKey))
	if config.PrivateKey != nil {
		s.router.Use(middleware.CryptoMiddleware(config.PrivateKey))
	}
	s.router.Use(middleware.GzipMiddleware)
	// Одинаково обрабатываем пути с завершающим слешем и без него.
	s.router.Use(chimiddleware.StripSlashes)

	updateHandler := NewUpdateHandler(s.storage, config.NotifyAudit)
	valueHandler := NewValueHandler(s.storage)
	listHandler := NewListHandler(s.storage)

	s.router.Get("/", listHandler.ServeHTTP)
	s.router.Post("/update", updateHandler.ServeJSON)
	s.router.Post("/updates", updateHandler.ServeBatch)
	s.router.Post("/update/{type}/{name}/{value}", updateHandler.ServeHTTP)
	s.router.Post("/value", valueHandler.ServeJSON)
	s.router.Get("/value/{type}/{name}", valueHandler.ServeHTTP)
	s.router.Get("/ping", s.ping)
}
