package handler

import (
	"context"
	"math"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/IvanSaratov/go-metrics-practice/internal/audit"
	models "github.com/IvanSaratov/go-metrics-practice/internal/model"
	"github.com/IvanSaratov/go-metrics-practice/internal/repository"
	"github.com/go-chi/chi/v5"
)

// AuditNotifier получает событие аудита после успешного сохранения метрик.
type AuditNotifier func(context.Context, audit.Event)

// UpdateHandler обрабатывает запросы на обновление метрик.
type UpdateHandler struct {
	storage     repository.Storage
	notifyAudit AuditNotifier
}

// NewUpdateHandler создаёт обработчик обновления метрик.
// Если notifyAudit равен nil, аудит отключён.
func NewUpdateHandler(storage repository.Storage, notifyAudit AuditNotifier) *UpdateHandler {
	return &UpdateHandler{
		storage:     storage,
		notifyAudit: notifyAudit,
	}
}

// ServeHTTP сохраняет метрику из адреса POST /update/{type}/{name}/{value}.
// Значение gauge заменяется, а counter — прибавляется к текущему.
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
		h.auditRequest(r, metricName)
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
		h.auditRequest(r, metricName)
		w.WriteHeader(http.StatusOK)
		return
	}

	w.WriteHeader(http.StatusBadRequest)
}

// ServeJSON сохраняет метрику из JSON в запросе POST /update.
// В ответе возвращает метрику с новым значением, для counter — накопленную сумму.
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

	h.auditRequest(r, metric.ID)
	writeJSON(w, http.StatusOK, metric)
}

// ServeBatch сохраняет JSON-массив метрик из запроса POST /updates.
// В ответе возвращает принятый пакет. Пустой массив допустим.
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

	names := make([]string, len(metrics))
	for index, metric := range metrics {
		names[index] = metric.ID
	}
	h.auditRequest(r, names...)
	writeJSON(w, http.StatusOK, metrics)
}

// Передаёт в аудит только имена сохранённых метрик
func (h *UpdateHandler) auditRequest(r *http.Request, names ...string) {
	if h.notifyAudit == nil || len(names) == 0 {
		return
	}
	address := r.RemoteAddr
	if host, _, err := net.SplitHostPort(address); err == nil {
		address = host
	}
	h.notifyAudit(r.Context(), audit.Event{
		Timestamp: time.Now().Unix(),
		Metrics:   names,
		IPAddress: address,
	})
}
