package grafana

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

//go:generate mockgen -package mock -destination mock/client.mock.go github.com/ethpandaops/panda-pulse/pkg/grafana Client

const (
	DefaultGrafanaBaseURL   = "https://grafana.observability.ethpandaops.io"
	DefaultPromDatasourceID = "UhcO3vy7z"
)

// Client is the interface for Grafana operations.
type Client interface {
	// Query executes a Grafana query.
	Query(ctx context.Context, query string) (*QueryResponse, error)
	// GetNetworks fetches the list of networks from Grafana.
	GetNetworks(ctx context.Context) ([]string, error)
	// GetBaseURL returns the base URL of the Grafana instance.
	GetBaseURL() string
}

// client is a Grafana client implementation of Client.
type client struct {
	baseURL      string
	dataSourceID string
	apiKey       string
	httpClient   *http.Client
}

// NewClient creates a new Grafana client.
func NewClient(cfg *Config, httpClient *http.Client) Client {
	return &client{
		baseURL:      cfg.BaseURL,
		dataSourceID: cfg.PromDatasourceID,
		apiKey:       cfg.Token,
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
		return nil, fmt.Errorf("unexpected status code %d", resp.StatusCode)
	}

	var response QueryResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &response, nil
}

// GetNetworks fetches the list of networks from Grafana.
func (c *client) GetNetworks(ctx context.Context) ([]string, error) {
	payload := map[string]interface{}{
		"queries": []map[string]interface{}{
			{
				"refId": "networks",
				"datasource": map[string]interface{}{
					"uid": c.dataSourceID,
				},
				"expr":          "count by (network) (up)",
				"maxDataPoints": 1,
				"intervalMs":    60000,
				"interval":      "1m",
			},
		},
		"from": "now-5m",
		"to":   "now",
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal network query payload: %w", err)
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

	var response struct {
		Results map[string]struct {
			Frames []struct {
				Data struct {
					Values [][]interface{} `json:"values"`
				} `json:"data"`
				Schema struct {
					Fields []struct {
						Labels map[string]string `json:"labels"`
					} `json:"fields"`
				} `json:"schema"`
			} `json:"frames"`
		} `json:"results"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	networks := make([]string, 0)

	if result, ok := response.Results["networks"]; ok {
		for _, frame := range result.Frames {
			for _, field := range frame.Schema.Fields {
				if network, ok := field.Labels["network"]; ok {
					if strings.Contains(network, "-devnet-") {
						networks = append(networks, network)
					}
				}
			}
		}
	}

	return networks, nil
}

// GetBaseURL returns the base URL of the Grafana instance.
func (c *client) GetBaseURL() string {
	return c.baseURL
}
