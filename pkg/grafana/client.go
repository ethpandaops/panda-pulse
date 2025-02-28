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
	defaultMaxDataPoints    = 1
	defaultIntervalMs       = 60000
	defaultInterval         = "1m"
	defaultTimeRange        = "now-5m"
	defaultTimeTo           = "now"
	apiPath                 = "/api/ds/query"
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
	req, err := c.createRequest(ctx, "pandaPulse", query, "({{ingress_user}}) {{instance}}")
	if err != nil {
		return nil, err
	}

	body, err := c.doRequest(req)
	if err != nil {
		return nil, err
	}

	var response QueryResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &response, nil
}

// GetNetworks fetches the list of networks from Grafana.
func (c *client) GetNetworks(ctx context.Context) ([]string, error) {
	req, err := c.createRequest(ctx, "networks", "count by (network) (up)", "")
	if err != nil {
		return nil, err
	}

	body, err := c.doRequest(req)
	if err != nil {
		return nil, err
	}

	var response networkResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	networks := make([]string, 0)

	if result, ok := response.Results["networks"]; ok {
		for _, frame := range result.Frames {
			for _, field := range frame.Schema.Fields {
				if network, ok := field.Labels["network"]; ok && strings.Contains(network, "-devnet-") {
					networks = append(networks, network)
				}
			}
		}
	}

	return networks, nil
}

func (c *client) createRequest(ctx context.Context, refID, expr, legendFormat string) (*http.Request, error) {
	payload := queryPayload{
		Queries: []query{
			{
				RefID: refID,
				Datasource: map[string]interface{}{
					"uid": c.dataSourceID,
				},
				Expr:          expr,
				MaxDataPoints: defaultMaxDataPoints,
				IntervalMs:    defaultIntervalMs,
				Interval:      defaultInterval,
				LegendFormat:  legendFormat,
			},
		},
		From: defaultTimeRange,
		To:   defaultTimeTo,
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+apiPath, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	return req, nil
}

func (c *client) doRequest(req *http.Request) ([]byte, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

// GetBaseURL returns the base URL of the Grafana instance.
func (c *client) GetBaseURL() string {
	return c.baseURL
}
