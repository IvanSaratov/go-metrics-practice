package agent

import (
	"fmt"
	"net/http"
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
	return c.sendMetric("gauge", name, fmt.Sprintf("%v", value))
}

func (c *Client) SendCounter(name string, value int64) error {
	return c.sendMetric("counter", name, fmt.Sprintf("%d", value))
}

func (c *Client) sendMetric(metricType string, name string, value string) error {
	url := fmt.Sprintf("%s/update/%s/%s/%s", c.baseURL, metricType, name, value)

	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/plain")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}
