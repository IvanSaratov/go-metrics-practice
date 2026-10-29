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

// Отправляет HTTP-запросы к удалённому приёмнику аудита.
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// Передаёт события аудита удалённому серверу.
type HTTPObserver struct {
	url    string
	client HTTPClient
}

// Проверяет адрес и создаёт HTTP-приёмник с заданным клиентом.
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

// Отправляет событие как JSON и ограничивает время запроса.
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
