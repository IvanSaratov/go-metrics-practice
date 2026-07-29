package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"

	models "github.com/IvanSaratov/go-metrics-practice/internal/model"
)

const jsonContentType = "application/json"

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

func (c *Client) sendMetric(metric models.Metrics) error {
	body, err := json.Marshal(metric)
	if err != nil {
		return fmt.Errorf("encode metric: %w", err)
	}

	req, err := http.NewRequest(
		http.MethodPost,
		c.baseURL+"/update",
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", jsonContentType)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send metric: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || mediaType != jsonContentType {
		_, _ = io.Copy(io.Discard, resp.Body)
		return fmt.Errorf(
			"unexpected Content-Type: %q",
			resp.Header.Get("Content-Type"),
		)
	}

	// Полностью вычитываем ответ, чтобы HTTP-соединение можно было переиспользовать
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	return nil
}
