package middleware

import (
	"bytes"
	"io"
	"net/http"

	"github.com/IvanSaratov/go-metrics-practice/internal/signature"
)

// проверяет подпись запроса и подписывает тело ответа
// поскольку проект чисто учебнй - то будем шифровать уже готовые gzip байты
func SignatureMiddleware(key string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if key == "" {
			// Без ключа сохраняем исходный поток обработки без буферизации
			// passthrout
			return next
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			response := newBufferedResponseWriter()
			// Отсутствие заголовка допустимо, проверяем только переданную подпись
			encodedHash := r.Header.Get(signature.HeaderName)
			if encodedHash != "" {
				body, err := io.ReadAll(r.Body)
				_ = r.Body.Close()
				if err != nil || !signature.Verify(body, key, encodedHash) {
					http.Error(response, "invalid request signature", http.StatusBadRequest)
					writeSignedResponse(w, response, key)
					return
				}

				// Восстанавливаем тело после проверки для следующего middleware/обработчика
				r.Body = io.NopCloser(bytes.NewReader(body))
			}

			next.ServeHTTP(response, r)
			writeSignedResponse(w, response, key)
		})
	}
}

// сохраняет ответ до вычисления его подписи
type bufferedResponseWriter struct {
	header     http.Header
	body       bytes.Buffer
	statusCode int
}

// создаёт пустой буфер ответа
func newBufferedResponseWriter() *bufferedResponseWriter {
	return &bufferedResponseWriter{
		header: make(http.Header),
	}
}

// возвращает заголовки буферизованного ответа
func (w *bufferedResponseWriter) Header() http.Header {
	return w.header
}

// сохраняет первый установленный HTTP-статус
func (w *bufferedResponseWriter) WriteHeader(statusCode int) {
	if w.statusCode == 0 {
		w.statusCode = statusCode
	}
}

// сохраняет тело и неявно устанавливает статус 200
func (w *bufferedResponseWriter) Write(data []byte) (int, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	return w.body.Write(data)
}

// добавляет подпись и отправляет накопленный ответ
func writeSignedResponse(destination http.ResponseWriter, source *bufferedResponseWriter, key string) {
	for name, values := range source.header {
		destination.Header()[name] = append([]string(nil), values...)
	}
	destination.Header().Set(signature.HeaderName, signature.Sum(source.body.Bytes(), key))

	statusCode := source.statusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	destination.WriteHeader(statusCode)
	_, _ = destination.Write(source.body.Bytes())
}
