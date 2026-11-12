package middleware

import (
	"bytes"
	"crypto/rsa"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/IvanSaratov/go-metrics-practice/internal/cryptoutil"
)

// CryptoMiddleware расшифровывает запросы с внешним слоем rsa.
// Обычные запросы пропускает дальше, даже если приватный ключ задан.
func CryptoMiddleware(privateKey *rsa.PrivateKey) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Кодировки снимаются в обратном порядке: последняя применяется к внешнему слою.
			encoding := strings.ToLower(strings.Join(r.Header.Values("Content-Encoding"), ","))
			encodings := strings.Split(encoding, ",")
			last := len(encodings) - 1
			if strings.TrimSpace(encodings[last]) != cryptoutil.Encoding {
				next.ServeHTTP(w, r)
				return
			}

			// Без ключа запрос заведомо не расшифровать, поэтому не читаем тело в память.
			if privateKey == nil {
				_ = r.Body.Close()
				http.Error(w, "invalid encrypted body", http.StatusBadRequest)
				return
			}

			body, err := io.ReadAll(r.Body)
			_ = r.Body.Close()
			if err != nil {
				http.Error(w, "invalid encrypted body", http.StatusBadRequest)
				return
			}
			// Чужой ключ и повреждённое тело дают одинаковый HTTP-ответ.
			body, err = cryptoutil.Decrypt(privateKey, body)
			if err != nil {
				http.Error(w, "invalid encrypted body", http.StatusBadRequest)
				return
			}

			// Следующий middleware получит расшифрованное тело и его новую длину.
			r.Body = io.NopCloser(bytes.NewReader(body))
			r.ContentLength = int64(len(body))
			r.Header.Set("Content-Length", strconv.Itoa(len(body)))
			r.GetBody = nil // Старый GetBody мог бы вернуть шифротекст.

			// Убираем только слой шифрования. Оставшийся gzip распакует GzipMiddleware.
			remaining := strings.TrimSpace(strings.Join(encodings[:last], ","))
			if remaining == "" {
				r.Header.Del("Content-Encoding")
			} else {
				r.Header.Set("Content-Encoding", remaining)
			}
			next.ServeHTTP(w, r)
		})
	}
}
