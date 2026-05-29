// Package roll performs gated, sequential container image rollouts across an
// Ethereum node fleet. Targets are resolved from cartographoor's published
// per-network inventory, health is gated on Dora, and rolls are triggered via
// each node's watchtower HTTP API vhost.
package roll

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// DefaultInventoryBaseURL is where cartographoor publishes per-network inventory.
const DefaultInventoryBaseURL = "https://ethpandaops-platform-production-cartographoor.ams3.digitaloceanspaces.com"

// clientInfo mirrors the fields we need from a cartographoor inventory client.
type clientInfo struct {
	ClientName string `json:"clientName"`
	ClientType string `json:"clientType"`
}

type inventoryData struct {
	Network          string       `json:"network"`
	ConsensusClients []clientInfo `json:"consensusClients"`
	ExecutionClients []clientInfo `json:"executionClients"`
}

// Target is a single node to roll.
type Target struct {
	// Name is the node identifier (e.g. lighthouse-ethrex-1).
	Name string
	// Clients are the client types running on this node (CL and EL).
	Clients []string
	// tokens are the lowercased selectable identifiers for this node — node
	// name, client types, and the cl_el group — used by the --client matcher.
	tokens []string
}

// FetchInventory loads and parses a network's inventory from cartographoor.
func FetchInventory(ctx context.Context, baseURL, network string) (*inventoryData, error) {
	if baseURL == "" {
		baseURL = DefaultInventoryBaseURL
	}

	endpoint := fmt.Sprintf("%s/inventory/%s.json", strings.TrimRight(baseURL, "/"), network)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch inventory for %s: %w", network, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("inventory for %s: status %d", network, resp.StatusCode)
	}

	var data inventoryData
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("decode inventory for %s: %w", network, err)
	}

	return &data, nil
}

// ResolveTargets groups the inventory by node into roll targets, computing match
// tokens (node name, CL/EL client types, and the cl_el group) for selection.
func ResolveTargets(inv *inventoryData) []Target {
	type agg struct {
		clTypes []string
		elTypes []string
	}

	byNode := map[string]*agg{}
	order := []string{}

	get := func(name string) *agg {
		a, ok := byNode[name]
		if !ok {
			a = &agg{}
			byNode[name] = a
			order = append(order, name)
		}

		return a
	}

	for _, c := range inv.ConsensusClients {
		if c.ClientName == "" || c.ClientType == "" {
			continue
		}

		a := get(c.ClientName)
		a.clTypes = append(a.clTypes, c.ClientType)
	}

	for _, c := range inv.ExecutionClients {
		if c.ClientName == "" || c.ClientType == "" {
			continue
		}

		a := get(c.ClientName)
		a.elTypes = append(a.elTypes, c.ClientType)
	}

	targets := make([]Target, 0, len(order))

	for _, name := range order {
		a := byNode[name]

		clients := make([]string, 0, len(a.clTypes)+len(a.elTypes))
		clients = append(clients, a.clTypes...)
		clients = append(clients, a.elTypes...)

		targets = append(targets, Target{
			Name:    name,
			Clients: clients,
			tokens:  buildTokens(name, a.clTypes, a.elTypes),
		})
	}

	sort.Slice(targets, func(i, j int) bool { return targets[i].Name < targets[j].Name })

	return targets
}

// buildTokens returns the lowercased, deduped set of selectable tokens for a
// node: its name, each client type, and each cl_el group pairing.
func buildTokens(name string, clTypes, elTypes []string) []string {
	seen := map[string]bool{}
	tokens := []string{}

	add := func(s string) {
		s = strings.ToLower(strings.TrimSpace(s))
		if s != "" && !seen[s] {
			seen[s] = true
			tokens = append(tokens, s)
		}
	}

	add(name)

	for _, cl := range clTypes {
		add(cl)
	}

	for _, el := range elTypes {
		add(el)
	}

	for _, cl := range clTypes {
		for _, el := range elTypes {
			add(cl + "_" + el)
		}
	}

	return tokens
}
