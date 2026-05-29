package roll

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
)

const defaultProviderTTL = 30 * time.Second

// InventoryProvider fetches and caches per-network targets from cartographoor,
// so autocomplete and rollouts don't refetch on every Discord interaction.
type InventoryProvider struct {
	baseURL string
	ttl     time.Duration

	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	targets []Target
	fetched time.Time
}

// NewInventoryProvider returns a provider. Empty baseURL uses the default;
// non-positive ttl uses a 30s default.
func NewInventoryProvider(baseURL string, ttl time.Duration) *InventoryProvider {
	if baseURL == "" {
		baseURL = DefaultInventoryBaseURL
	}

	if ttl <= 0 {
		ttl = defaultProviderTTL
	}

	return &InventoryProvider{
		baseURL: baseURL,
		ttl:     ttl,
		cache:   map[string]cacheEntry{},
	}
}

// Targets returns the cached targets for a network, refreshing if stale.
func (p *InventoryProvider) Targets(ctx context.Context, network string) ([]Target, error) {
	p.mu.Lock()
	if e, ok := p.cache[network]; ok && time.Since(e.fetched) < p.ttl {
		targets := e.targets
		p.mu.Unlock()

		return targets, nil
	}
	p.mu.Unlock()

	inv, err := FetchInventory(ctx, p.baseURL, network)
	if err != nil {
		return nil, err
	}

	targets := ResolveTargets(inv)

	p.mu.Lock()
	p.cache[network] = cacheEntry{targets: targets, fetched: time.Now()}
	p.mu.Unlock()

	return targets, nil
}

// Suggest returns up to limit selectable identifiers for a network's hosts,
// filtered by input/scope (see Suggestions).
func (p *InventoryProvider) Suggest(ctx context.Context, network, input, scope string, limit int) ([]string, error) {
	targets, err := p.Targets(ctx, network)
	if err != nil {
		return nil, err
	}

	return Suggestions(targets, input, scope, limit), nil
}

// Suggestions computes the selectable token list from resolved targets: group
// names (client types and cl_el pairings), node names, and "all". Results are
// filtered to those containing the case-insensitive input substring, and — when
// scope is non-empty — to those containing scope (e.g. only lighthouse-related
// tokens when a roll is triggered from a lighthouse build). Sorted, capped at
// limit.
func Suggestions(targets []Target, input, scope string, limit int) []string {
	input = strings.ToLower(strings.TrimSpace(input))
	scope = strings.ToLower(strings.TrimSpace(scope))

	seen := map[string]bool{}
	out := []string{}

	add := func(tok string) {
		if tok == "" || seen[tok] {
			return
		}

		if scope != "" && !strings.Contains(tok, scope) {
			return
		}

		if input != "" && !strings.Contains(tok, input) {
			return
		}

		seen[tok] = true
		out = append(out, tok)
	}

	add("all")

	for _, t := range targets {
		for _, tok := range t.tokens {
			add(tok)
		}
	}

	sort.Strings(out)

	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}

	return out
}
