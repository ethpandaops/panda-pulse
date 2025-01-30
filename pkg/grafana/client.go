package grafana

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const (
	prometheusDatasourceID = "UhcO3vy7z"
	grafanaBaseURL         = "https://grafana.observability.ethpandaops.io"
)

// Client is a Grafana client.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// QueryResponse is the response from a Grafana query.
type QueryResponse struct {
	Results struct {
		PandaPulse struct {
			Frames []struct {
				Schema struct {
					Fields []struct {
						Labels map[string]string `json:"labels"`
					} `json:"fields"`
				} `json:"schema"`
				Data struct {
					Values []interface{} `json:"values"`
				} `json:"data"`
			} `json:"frames"`
		} `json:"pandaPulse"`
	} `json:"results"`
}

// NewClient creates a new Grafana client.
func NewClient(apiKey string, httpClient *http.Client) *Client {
	return &Client{
		baseURL:    grafanaBaseURL,
		apiKey:     apiKey,
		httpClient: httpClient,
	}
}

// Query executes a Grafana query.
func (c *Client) Query(ctx context.Context, query string) (*QueryResponse, error) {
	payload := map[string]interface{}{
		"queries": []map[string]interface{}{
			{
				"refId": "pandaPulse",
				"datasource": map[string]interface{}{
					"uid": prometheusDatasourceID,
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
