// Package handler содержит HTTP-обработчики для работы с метриками.
package handler

import (
	"net/http"

	"github.com/IvanSaratov/go-metrics-practice/internal/repository"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

// Server направляет HTTP-запросы к обработчикам метрик и проверки базы.
type Server struct {
	router      *chi.Mux
	storage     repository.Storage
	db          Database
	notifyAudit AuditNotifier
}

// NewServer создаёт сервер и подключает маршруты.
// Если db равен nil, запрос /ping возвращает 500.
// Если notifyAudit равен nil, аудит отключён.
func NewServer(storage repository.Storage, db Database, notifyAudit AuditNotifier) *Server {
	server := &Server{
		router:      chi.NewRouter(),
		storage:     storage,
		db:          db,
		notifyAudit: notifyAudit,
	}
	server.registerRoutes()
	return server
}

// ServeHTTP передаёт запрос обработчику по его методу и пути.
func (s *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	s.router.ServeHTTP(writer, request)
}

func (s *Server) registerRoutes() {
	// Одинаково обрабатываем пути с завершающим слешем и без него.
	s.router.Use(chimiddleware.StripSlashes)

	updateHandler := NewUpdateHandler(s.storage, s.notifyAudit)
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
