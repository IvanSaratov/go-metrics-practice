package handler

import (
	"fmt"
	"net/http"

	"github.com/IvanSaratov/go-metrics-practice/internal/repository"
	"github.com/go-chi/chi/v5"
)

type ValueHandler struct {
	storage repository.Storage
}

func NewValueHandler(storage repository.Storage) *ValueHandler {
	return &ValueHandler{
		storage: storage,
	}
}

func (h *ValueHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	metricType := chi.URLParam(r, "type")
	metricName := chi.URLParam(r, "name")

	if metricName == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	if metricType == "gauge" {
		value, ok := h.storage.GetGauge(metricName)
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "%v", value)
		return
	}

	if metricType == "counter" {
		value, ok := h.storage.GetCounter(metricName)
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "%d", value)
		return
	}

	w.WriteHeader(http.StatusNotFound)
}
