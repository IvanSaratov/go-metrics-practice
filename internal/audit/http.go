package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const httpAuditTimeout = 5 * time.Second

// HTTPClient выполняет HTTP-запросы аудита. Подходит стандартный *http.Client.
type HTTPClient interface {
	// Do отправляет запрос и возвращает ответ сервера.
	// Тело ответа закрывает вызывающий код.
	Do(*http.Request) (*http.Response, error)
}

// HTTPObserver отправляет события аудита на HTTP-сервер.
type HTTPObserver struct {
	url    string
	client HTTPClient
}

// NewHTTPObserver проверяет адрес и создаёт приёмник аудита.
// В destination нужен полный адрес с http:// или https:// и именем хоста.
func NewHTTPObserver(destination string, client HTTPClient) (*HTTPObserver, error) {
	parsed, err := url.Parse(destination)
	if err != nil {
		return nil, fmt.Errorf("parse audit URL: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return nil, fmt.Errorf("audit URL must be an absolute HTTP(S) URL")
	}

	return &HTTPObserver{url: destination, client: client}, nil
}

// Update отправляет событие JSON-запросом POST с таймаутом 5 секунд.
// Успешными считает только ответы 2xx.
func (o *HTTPObserver) Update(ctx context.Context, event Event) error {
	ctx, cancel := context.WithTimeout(ctx, httpAuditTimeout)
	defer cancel()

	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode audit event: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, o.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create audit request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := o.client.Do(request)
	if err != nil {
		return fmt.Errorf("send audit event: %w", err)
	}
	defer response.Body.Close()

	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		return fmt.Errorf("read audit response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("unexpected audit response status: %d", response.StatusCode)
	}
	return nil
}
