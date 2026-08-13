package handler

import (
	"net/http"

	"github.com/IvanSaratov/go-metrics-practice/internal/repository"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

func NewRouter(storage repository.Storage) http.Handler {
	router := chi.NewRouter()
	// Одинаково обрабатываем пути с завершающим слешем и без него.
	router.Use(chimiddleware.StripSlashes)

	updateHandler := NewUpdateHandler(storage)
	valueHandler := NewValueHandler(storage)
	listHandler := NewListHandler(storage)

	router.Get("/", listHandler.ServeHTTP)
	router.Post("/update", updateHandler.ServeJSON)
	router.Post("/update/{type}/{name}/{value}", updateHandler.ServeHTTP)
	router.Post("/value", valueHandler.ServeJSON)
	router.Get("/value/{type}/{name}", valueHandler.ServeHTTP)

	return router
}
