package handler

import (
	"net/http"

	"github.com/IvanSaratov/go-metrics-practice/internal/repository"
	"github.com/go-chi/chi/v5"
)

func NewRouter(storage repository.Storage) http.Handler {
	router := chi.NewRouter()
	updateHandler := NewUpdateHandler(storage)
	valueHandler := NewValueHandler(storage)

	router.Post("/update/{type}/{name}/{value}", updateHandler.ServeHTTP)
	router.Get("/value/{type}/{name}", valueHandler.ServeHTTP)

	return router
}
