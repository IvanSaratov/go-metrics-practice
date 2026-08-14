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

		if err := h.storage.SetGauge(r.Context(), metricName, value); err != nil {
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

		if _, err := h.storage.AddCounter(r.Context(), metricName, value); err != nil {
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
	if err := metric.ValidateUpdate(); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	switch metric.MType {
	case models.Gauge:
		if err := h.storage.SetGauge(r.Context(), metric.ID, *metric.Value); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "failed to store metric")
			return
		}
	case models.Counter:
		// В ответе возвращаем уже накопленное значение counter
		total, err := h.storage.AddCounter(r.Context(), metric.ID, *metric.Delta)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "failed to store metric")
			return
		}
		metric.Delta = &total
	}

	writeJSON(w, http.StatusOK, metric)
}

func (h *UpdateHandler) ServeBatch(w http.ResponseWriter, r *http.Request) {
	var metrics []models.Metrics
	if requestError := decodeJSON(w, r, &metrics); requestError != nil {
		writeJSONError(w, requestError.status, requestError.message)
		return
	}
	if metrics == nil {
		writeJSONError(w, http.StatusBadRequest, "metrics batch must be an array")
		return
	}

	if err := models.ValidateUpdates(metrics); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.storage.UpdateBatch(r.Context(), metrics); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to store metrics")
		return
	}

	writeJSON(w, http.StatusOK, metrics)
}
