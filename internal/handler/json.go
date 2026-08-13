package handler

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
)

const (
	jsonContentType = "application/json"
	// Ограничение на слишком большой запрос
	maxJSONRequestSize = 1 << 20
)

type errorResponse struct {
	Error string `json:"error"`
}

type jsonRequestError struct {
	status  int
	message string
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) *jsonRequestError {
	// сделаем проверку на заголовок
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != jsonContentType {
		return &jsonRequestError{
			status:  http.StatusUnsupportedMediaType,
			message: "Content-Type must be application/json",
		}
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxJSONRequestSize)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		return classifyJSONError(err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return &jsonRequestError{
				status:  http.StatusBadRequest,
				message: "request body must contain a single JSON value",
			}
		}
		return classifyJSONError(err)
	}

	return nil
}

// Функция - обработчик ошибок и состояний
func classifyJSONError(err error) *jsonRequestError {
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		return &jsonRequestError{
			status:  http.StatusRequestEntityTooLarge,
			message: "request body is too large",
		}
	}

	return &jsonRequestError{
		status:  http.StatusBadRequest,
		message: "invalid JSON",
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		status = http.StatusInternalServerError
		body, _ = json.Marshal(errorResponse{Error: "failed to encode response"})
	}
	body = append(body, '\n')

	w.Header().Set("Content-Type", jsonContentType)
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}
