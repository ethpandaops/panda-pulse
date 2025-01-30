package grafana

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

//go:generate mockgen -package mock -destination mock/client.mock.go github.com/ethpandaops/panda-pulse/pkg/grafana GrafanaClient

// GrafanaClient is the interface for Grafana operations.
type GrafanaClient interface {
	// Query executes a Grafana query.
	Query(ctx context.Context, query string) (*QueryResponse, error)
}

// client is a Grafana client implementation of GrafanaClient.
type client struct {
	baseURL      string
	dataSourceID string
	apiKey       string
	httpClient   *http.Client
}

// NewClient creates a new Grafana client.
func NewClient(baseURL string, dataSourceID string, apiKey string, httpClient *http.Client) GrafanaClient {
	return &client{
		baseURL:      baseURL,
		dataSourceID: dataSourceID,
		apiKey:       apiKey,
		httpClient:   httpClient,
	}
}

// Query executes a Grafana query.
func (c *client) Query(ctx context.Context, query string) (*QueryResponse, error) {
	payload := map[string]interface{}{
		"queries": []map[string]interface{}{
			{
				"refId": "pandaPulse",
				"datasource": map[string]interface{}{
					"uid": c.dataSourceID,
				},
				"expr":          query,
				"maxDataPoints": 1,
				"intervalMs":    60000,
				"interval":      "1m",
				"legendFormat":  "({{ingress_user}}) {{instance}}",
			},
		},
		"from": "now-5m",
		"to":   "now",
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal query payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/ds/query", bytes.NewBuffer(jsonPayload))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	var response QueryResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &response, nil
}
