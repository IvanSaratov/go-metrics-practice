package handler

import (
	"net/http"

	models "github.com/IvanSaratov/go-metrics-practice/internal/model"
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
		_, _ = w.Write([]byte(formatGauge(value)))
		return
	}

	if metricType == "counter" {
		value, ok := h.storage.GetCounter(metricName)
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(formatCounter(value)))
		return
	}

	w.WriteHeader(http.StatusNotFound)
}

// ServeJSON возвращает актуальное значение запрошенной метрики
func (h *ValueHandler) ServeJSON(w http.ResponseWriter, r *http.Request) {
	var metric models.Metrics
	if requestError := decodeJSON(w, r, &metric); requestError != nil {
		writeJSONError(w, requestError.status, requestError.message)
		return
	}
	if metric.ID == "" {
		writeJSONError(w, http.StatusBadRequest, "metric id is required")
		return
	}
	if metric.Value != nil || metric.Delta != nil {
		writeJSONError(w, http.StatusBadRequest, "metric value must be omitted")
		return
	}

	switch metric.MType {
	case models.Gauge:
		value, ok := h.storage.GetGauge(metric.ID)
		if !ok {
			writeJSONError(w, http.StatusNotFound, "metric not found")
			return
		}
		metric.Value = &value
		metric.Delta = nil
	case models.Counter:
		value, ok := h.storage.GetCounter(metric.ID)
		if !ok {
			writeJSONError(w, http.StatusNotFound, "metric not found")
			return
		}
		metric.Delta = &value
		metric.Value = nil
	default:
		writeJSONError(w, http.StatusBadRequest, "unsupported metric type")
		return
	}

	writeJSON(w, http.StatusOK, metric)
}
