package handler

import (
	"net/http"

	"github.com/IvanSaratov/go-metrics-practice/internal/repository"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

type Server struct {
	router  *chi.Mux
	storage repository.Storage
	db      Database
}

func NewServer(storage repository.Storage, db Database) *Server {
	server := &Server{
		router:  chi.NewRouter(),
		storage: storage,
		db:      db,
	}
	server.registerRoutes()
	return server
}

func (s *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	s.router.ServeHTTP(writer, request)
}

func (s *Server) registerRoutes() {
	// Одинаково обрабатываем пути с завершающим слешем и без него.
	s.router.Use(chimiddleware.StripSlashes)

	updateHandler := NewUpdateHandler(s.storage)
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
