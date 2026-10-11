package roll

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// doraReadyStatus is Dora's status for a fully-synced, healthy node.
	doraReadyStatus = "ready"
	doraCacheTTL    = 3 * time.Second
)

// DoraHealth determines node health from a Dora explorer — the source of truth
// the rest of the ethpandaops stack already uses. One API call covers the whole
// fleet, and the endpoint is unauthenticated, so it avoids per-node basic-auth
// beacon calls. Dora's status already accounts for the wall-clock head, so a
// node reporting "ready" is genuinely caught up.
type DoraHealth struct {
	baseURL    string
	httpClient *http.Client

	mu      sync.Mutex
	cache   map[string]doraClient
	fetched time.Time
}

//nolint:tagliatelle // Dora API uses snake_case
type doraClient struct {
	Name     string `json:"client_name"`
	Status   string `json:"status"`
	HeadSlot uint64 `json:"head_slot"`
}

type doraClientsResponse struct {
	Clients []doraClient `json:"clients"`
}

// NewDoraHealth returns a DoraHealth checker for the given Dora base URL.
func NewDoraHealth(baseURL string) *DoraHealth {
	return &DoraHealth{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// DoraURLForNetwork returns the conventional Dora URL for an ethpandaops network
// (e.g. "glamsterdam-devnet-4" -> https://dora.glamsterdam-devnet-4.ethpandaops.io).
func DoraURLForNetwork(network string) string {
	return fmt.Sprintf("https://dora.%s.ethpandaops.io", network)
}

// Healthy reports whether the named node is "ready" according to Dora.
func (d *DoraHealth) Healthy(ctx context.Context, node string) (bool, string, error) {
	clients, err := d.clients(ctx)
	if err != nil {
		return false, "", err
	}

	c, ok := clients[node]
	if !ok {
		return false, "not found in dora", nil
	}

	if !strings.EqualFold(c.Status, doraReadyStatus) {
		return false, fmt.Sprintf("dora status=%s (head=%d)", c.Status, c.HeadSlot), nil
	}

	return true, fmt.Sprintf("ready (head=%d)", c.HeadSlot), nil
}

func (d *DoraHealth) clients(ctx context.Context) (map[string]doraClient, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.cache != nil && time.Since(d.fetched) < doraCacheTTL {
		return d.cache, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.baseURL+"/api/v1/clients/consensus", nil)
	if err != nil {
		return nil, err
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dora clients: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dora clients returned %d", resp.StatusCode)
	}

	var parsed doraClientsResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode dora clients: %w", err)
	}

	clients := make(map[string]doraClient, len(parsed.Clients))
	for _, c := range parsed.Clients {
		clients[c.Name] = c
	}

	d.cache = clients
	d.fetched = time.Now()

	return clients, nil
}
