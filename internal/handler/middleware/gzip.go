package middleware

import (
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

const (
	gzipEncoding     = "gzip"
	identityEncoding = "identity"
)

// GzipMiddleware распаковывает gzip-запросы и сжимает JSON/HTML-ответы
func GzipMiddleware(next http.Handler) http.Handler {
	// Используем встроенный комрессор в либе
	compressResponses := chimiddleware.Compress(
		gzip.DefaultCompression,
		"application/json",
		"text/html",
	)(next)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Проверка заголовка
		encoding := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Encoding")))

		switch encoding {
		case "", identityEncoding:
			serveCompressedResponse(compressResponses, w, r)
			return
		case gzipEncoding:
		default:
			// Исключительная ситуация
			_ = r.Body.Close()
			http.Error(w, "unsupported Content-Encoding", http.StatusUnsupportedMediaType)
			return
		}

		reader, err := gzip.NewReader(r.Body)
		if err != nil {
			_ = r.Body.Close()
			http.Error(w, "invalid gzip body", http.StatusBadRequest)
			return
		}

		body := &gzipRequestBody{
			Reader: reader,
			source: r.Body,
		}
		r.Body = body
		r.ContentLength = -1
		r.GetBody = nil
		r.Header.Del("Content-Encoding")
		r.Header.Del("Content-Length")
		defer body.Close()

		serveCompressedResponse(compressResponses, w, r)
	})
}

// оставляет встроенному Chi компресеру только выбор между gzip и несжатым ответом
func serveCompressedResponse(next http.Handler, w http.ResponseWriter, r *http.Request) {
	request := r.Clone(r.Context())

	if acceptsGzip(r.Header.Values("Accept-Encoding")) {
		request.Header.Set("Accept-Encoding", gzipEncoding)
	} else {
		request.Header.Del("Accept-Encoding")
	}

	next.ServeHTTP(w, request)
}

// учитывает приоритет явного gzip над wildcard и его q-значение
func acceptsGzip(values []string) bool {
	var (
		gzipSeen         bool
		gzipAccepted     bool
		wildcardSeen     bool
		wildcardAccepted bool
	)

	for _, value := range values {
		for token := range strings.SplitSeq(value, ",") {
			encoding, accepted := parseAcceptedEncoding(token)

			switch encoding {
			case gzipEncoding:
				gzipSeen = true
				gzipAccepted = accepted
			case "*":
				wildcardSeen = true
				wildcardAccepted = accepted
			}
		}
	}

	if gzipSeen {
		return gzipAccepted
	}

	return wildcardSeen && wildcardAccepted
}

func parseAcceptedEncoding(token string) (string, bool) {
	parts := strings.Split(token, ";")
	encoding := strings.ToLower(strings.TrimSpace(parts[0]))
	quality := 1.0

	for _, parameter := range parts[1:] {
		name, value, found := strings.Cut(strings.TrimSpace(parameter), "=")
		if !found || !strings.EqualFold(strings.TrimSpace(name), "q") {
			continue
		}

		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil || parsed < 0 || parsed > 1 {
			return encoding, false
		}
		quality = parsed
	}

	return encoding, encoding != "" && quality > 0
}

type gzipRequestBody struct {
	*gzip.Reader
	source io.ReadCloser
	once   sync.Once
	err    error
}

func (b *gzipRequestBody) Close() error {
	b.once.Do(func() {
		b.err = errors.Join(b.Reader.Close(), b.source.Close())
	})
	return b.err
}
