package agent

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	models "github.com/IvanSaratov/go-metrics-practice/internal/model"
)

// Возможно не стоило выносить как константы
const (
	jsonContentType  = "application/json"
	gzipEncoding     = "gzip"
	identityEncoding = "identity"
)

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type Client struct {
	baseURL    string
	httpClient HTTPClient
}

func NewClient(baseURL string, httpClient HTTPClient) *Client {
	return &Client{
		baseURL:    baseURL,
		httpClient: httpClient,
	}
}

func (c *Client) SendGauge(name string, value float64) error {
	return c.sendMetric(models.Metrics{
		ID:    name,
		MType: models.Gauge,
		Value: &value,
	})
}

func (c *Client) SendCounter(name string, value int64) error {
	return c.sendMetric(models.Metrics{
		ID:    name,
		MType: models.Counter,
		Delta: &value,
	})
}

func (c *Client) SendBatch(metrics []models.Metrics) error {
	if len(metrics) == 0 {
		return nil
	}

	return c.send("/updates/", metrics)
}

func (c *Client) sendMetric(metric models.Metrics) error {
	return c.send("/update", metric)
}

func (c *Client) send(path string, payload any) error {
	body, err := encodeGzipJSON(payload)
	if err != nil {
		return fmt.Errorf("encode metric: %w", err)
	}

	req, err := http.NewRequest(
		http.MethodPost,
		c.baseURL+path,
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", jsonContentType)
	// Выставляем нужные заголовки
	req.Header.Set("Content-Encoding", gzipEncoding)
	req.Header.Set("Accept-Encoding", gzipEncoding)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send metric: %w", err)
	}
	defer resp.Body.Close()

	responseBody := io.Reader(resp.Body)
	var gzipReader *gzip.Reader

	switch encoding := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding"))); encoding {
	case "", identityEncoding:
	case gzipEncoding:
		gzipReader, err = gzip.NewReader(resp.Body)
		if err != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			return fmt.Errorf("decode gzip response: %w", err)
		}
		defer gzipReader.Close()
		responseBody = gzipReader
	default:
		_, _ = io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("unexpected Content-Encoding: %q", encoding)
	}

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, responseBody)
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || mediaType != jsonContentType {
		_, _ = io.Copy(io.Discard, responseBody)
		return fmt.Errorf(
			"unexpected Content-Type: %q",
			resp.Header.Get("Content-Type"),
		)
	}

	// Полностью вычитываем ответ, чтобы HTTP-соединение можно было переиспользовать
	if _, err := io.Copy(io.Discard, responseBody); err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	return nil
}

// Заменяем наш стандартный json преобразователь в отдельную функцию для кодирования
func encodeGzipJSON(value any) ([]byte, error) {
	var body bytes.Buffer
	writer := gzip.NewWriter(&body)

	if err := json.NewEncoder(writer).Encode(value); err != nil {
		_ = writer.Close()
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	return body.Bytes(), nil
}
