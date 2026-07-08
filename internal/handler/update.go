package handler

import (
	"net/http"
	"strconv"

	"github.com/IvanSaratov/go-metrics-practice/internal/repository"
	"github.com/go-chi/chi/v5"
)

type UpdateHandler struct {
	storage repository.Storage
}

func NewUpdateHandler(storage repository.Storage) *UpdateHandler {
	return &UpdateHandler{
		storage: storage,
	}
}

func (h *UpdateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	metricType := chi.URLParam(r, "type")
	metricName := chi.URLParam(r, "name")
	metricValue := chi.URLParam(r, "value")

	if metricName == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	if metricType == "gauge" {
		value, err := strconv.ParseFloat(metricValue, 64)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		h.storage.SetGauge(metricName, value)
		w.WriteHeader(http.StatusOK)
		return
	}

	if metricType == "counter" {
		value, err := strconv.ParseInt(metricValue, 10, 64)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		h.storage.AddCounter(metricName, value)
		w.WriteHeader(http.StatusOK)
		return
	}

	w.WriteHeader(http.StatusBadRequest)
}
