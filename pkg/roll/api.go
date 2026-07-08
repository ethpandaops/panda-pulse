package roll

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// WatchtowerURLForNode returns the watchtower API base URL for a node on an
// ethpandaops network, e.g.
// https://watchtower-lighthouse-ethrex-1.srv.glamsterdam-devnet-4.ethpandaops.io
func WatchtowerURLForNode(network, node string) string {
	return fmt.Sprintf("https://watchtower-%s.srv.%s.ethpandaops.io", node, network)
}

// APIActuator rolls by calling each node's watchtower HTTP API at its public
// vhost with a bearer token. The watchtower vhost is bearer-auth only.
type APIActuator struct {
	token      string
	network    string
	httpClient *http.Client
}

// NewAPIActuator returns an APIActuator for the given network and bearer token.
func NewAPIActuator(token, network string) *APIActuator {
	return &APIActuator{
		token:      token,
		network:    network,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

// Name implements Actuator.
func (a *APIActuator) Name() string { return "watchtower" }

// Roll implements Actuator: POST /v1/update to the node's watchtower vhost.
func (a *APIActuator) Roll(ctx context.Context, target Target, image string) error {
	endpoint := WatchtowerURLForNode(a.network, target.Name) + "/v1/update"
	if image != "" {
		endpoint += "?image=" + url.QueryEscape(image)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, http.NoBody)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+a.token)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("trigger update: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("watchtower returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return nil
}
