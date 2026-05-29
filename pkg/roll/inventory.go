// Package roll performs gated, sequential container image rollouts across an
// Ethereum node fleet. Targets are resolved from cartographoor's published
// per-network inventory; the roll itself is executed by a pluggable Actuator
// (SSH-to-local-watchtower by default).
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

// clientInfo mirrors the relevant fields of cartographoor's inventory ClientInfo.
type clientInfo struct {
	ClientName  string `json:"clientName"`
	ClientType  string `json:"clientType"`
	Version     string `json:"version"`
	DockerImage string `json:"dockerImage"`
	SSH         string `json:"ssh"`
	BeaconAPI   string `json:"bn"`
	RPC         string `json:"rpc"`
	Status      string `json:"status"`
}

type inventoryData struct {
	Network          string       `json:"network"`
	ConsensusClients []clientInfo `json:"consensusClients"`
	ExecutionClients []clientInfo `json:"executionClients"`
}

// Target is a single host to roll, grouped from the inventory by its SSH host.
type Target struct {
	// Name is the host/node identifier (derived from the SSH host).
	Name string
	// SSH is the cartographoor ssh value, e.g. "devops@host".
	SSH string
	// BeaconURL is the host's beacon API base URL for health gating (may be empty).
	BeaconURL string
	// Clients are the client names running on this host (CL and EL).
	Clients []string
	// tokens are the lowercased selectable identifiers for this host: node name,
	// client types, and the cl_el group. Used by the --limit/--client matcher.
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

// ResolveTargets groups the inventory by SSH host into roll targets, computing
// match tokens (node name, CL/EL client types, and the cl_el group) for
// Ansible-style selection. beaconScheme (e.g. "https") is prepended to the bare
// beacon hostname from the consensus client entry.
func ResolveTargets(inv *inventoryData, beaconScheme string) []Target {
	if beaconScheme == "" {
		beaconScheme = "https"
	}

	type agg struct {
		ssh     string
		beacon  string
		clients []string
		clTypes []string
		elTypes []string
	}

	byHost := map[string]*agg{}
	order := []string{}

	get := func(ssh string) *agg {
		a, ok := byHost[ssh]
		if !ok {
			a = &agg{ssh: ssh}
			byHost[ssh] = a
			order = append(order, ssh)
		}

		return a
	}

	for _, c := range inv.ConsensusClients {
		if c.SSH == "" {
			continue
		}

		a := get(c.SSH)
		if c.ClientName != "" {
			a.clients = append(a.clients, c.ClientName)
		}

		if c.ClientType != "" {
			a.clTypes = append(a.clTypes, c.ClientType)
		}

		if c.BeaconAPI != "" && a.beacon == "" {
			a.beacon = beaconScheme + "://" + c.BeaconAPI
		}
	}

	for _, c := range inv.ExecutionClients {
		if c.SSH == "" {
			continue
		}

		a := get(c.SSH)
		if c.ClientType != "" {
			a.elTypes = append(a.elTypes, c.ClientType)
		}
	}

	targets := make([]Target, 0, len(order))

	for _, ssh := range order {
		a := byHost[ssh]
		name := hostName(ssh)

		targets = append(targets, Target{
			Name:      name,
			SSH:       ssh,
			BeaconURL: a.beacon,
			Clients:   a.clients,
			tokens:    buildTokens(name, a.clTypes, a.elTypes),
		})
	}

	sort.Slice(targets, func(i, j int) bool { return targets[i].Name < targets[j].Name })

	return targets
}

// buildTokens returns the lowercased, deduped set of selectable tokens for a
// host: its node name, each client type, and each cl_el group pairing.
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

// hostName derives a short host identifier from an "user@host.domain" SSH value.
func hostName(ssh string) string {
	host := ssh
	if at := strings.Index(host, "@"); at >= 0 {
		host = host[at+1:]
	}

	if dot := strings.Index(host, "."); dot >= 0 {
		host = host[:dot]
	}

	return host
}
