package handler

import (
	"math"
	"net/http"
	"strconv"

	models "github.com/IvanSaratov/go-metrics-practice/internal/model"
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
		// JSON не поддерживает NaN и бесконечность (BUG)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if err := h.storage.SetGauge(metricName, value); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	if metricType == "counter" {
		value, err := strconv.ParseInt(metricValue, 10, 64)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if _, err := h.storage.AddCounter(metricName, value); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	w.WriteHeader(http.StatusBadRequest)
}

// ServeJSON сохраняет одну метрику, переданную в JSON
func (h *UpdateHandler) ServeJSON(w http.ResponseWriter, r *http.Request) {
	var metric models.Metrics
	if requestError := decodeJSON(w, r, &metric); requestError != nil {
		writeJSONError(w, requestError.status, requestError.message)
		return
	}
	if metric.ID == "" {
		writeJSONError(w, http.StatusBadRequest, "metric id is required")
		return
	}

	switch metric.MType {
	case models.Gauge:
		if metric.Value == nil || metric.Delta != nil {
			writeJSONError(w, http.StatusBadRequest, "gauge requires value only")
			return
		}
		if err := h.storage.SetGauge(metric.ID, *metric.Value); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "failed to store metric")
			return
		}
	case models.Counter:
		if metric.Delta == nil || metric.Value != nil {
			writeJSONError(w, http.StatusBadRequest, "counter requires delta only")
			return
		}
		// В ответе возвращаем уже накопленное значение counter
		total, err := h.storage.AddCounter(metric.ID, *metric.Delta)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "failed to store metric")
			return
		}
		metric.Delta = &total
	default:
		writeJSONError(w, http.StatusBadRequest, "unsupported metric type")
		return
	}

	writeJSON(w, http.StatusOK, metric)
}
