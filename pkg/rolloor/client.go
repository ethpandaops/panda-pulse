// Package rolloor reads rollout history from the rolloor instance a devnet
// runs, if it runs one.
package rolloor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"sync"
	"time"
)

// DefaultURLPattern is where a network's rolloor answers. Devnets adopt
// rolloor one at a time, so a network runs it exactly when this answers.
const DefaultURLPattern = "https://rolloor.%s.ethpandaops.io"

// probeTTL is how long an availability answer is trusted.
const probeTTL = 5 * time.Minute

// HistoryHeader names the response header carrying a history's identity,
// which changes when the database holding it is recreated.
const HistoryHeader = "Rolloor-History"

var networkPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$`)

// Event is one line of a rolloor's history.
type Event struct {
	ID       int64     `json:"id"`
	At       time.Time `json:"at"`
	Actor    string    `json:"actor"`
	Action   string    `json:"action"`
	Group    string    `json:"group"`
	Rollout  string    `json:"rollout"`
	Target   string    `json:"target"`
	Selector string    `json:"selector"`
	Reason   string    `json:"reason"`
}

// Page is part of a history, with the identity of the history it came from;
// History is empty when rolloor does not say.
type Page struct {
	Events  []Event
	History string
}

// Position is a history's newest event id, zero when it has none, and the
// history's identity.
type Position struct {
	ID      int64
	History string
}

// Client talks to the rolloor of any network.
type Client struct {
	http    *http.Client
	pattern string
	now     func() time.Time

	mu     sync.Mutex
	probes map[string]probe
}

type probe struct {
	ok bool
	at time.Time
}

// NewClient returns a client for the default URL pattern.
func NewClient(httpClient *http.Client) *Client {
	return NewClientWithPattern(httpClient, DefaultURLPattern)
}

// NewClientWithPattern returns a client for rolloor instances at pattern,
// a format string taking the network name.
func NewClientWithPattern(httpClient *http.Client, pattern string) *Client {
	return &Client{http: httpClient, pattern: pattern, now: time.Now, probes: map[string]probe{}}
}

// URL is a network's rolloor base URL.
func (c *Client) URL(network string) string {
	return fmt.Sprintf(c.pattern, network)
}

// Available reports whether a network runs rolloor, asking its health
// endpoint at most once per probeTTL.
func (c *Client) Available(ctx context.Context, network string) bool {
	if !networkPattern.MatchString(network) {
		return false
	}

	c.mu.Lock()
	p, seen := c.probes[network]
	c.mu.Unlock()

	if seen && c.now().Sub(p.at) < probeTTL {
		return p.ok
	}

	ok := c.answers(ctx, network)

	// An answer cut short by the caller's deadline says nothing about rolloor.
	if ctx.Err() != nil {
		return false
	}

	c.mu.Lock()
	c.probes[network] = probe{ok: ok, at: c.now()}
	c.mu.Unlock()

	return ok
}

func (c *Client) answers(ctx context.Context, network string) bool {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	var health struct {
		OK          bool   `json:"ok"`
		Environment string `json:"environment"`
	}

	if _, err := c.get(ctx, network, "/healthz", &health); err != nil {
		return false
	}

	return health.OK && health.Environment != ""
}

// History returns the events after an id, oldest first, including after zero.
func (c *Client) History(ctx context.Context, network string, after int64, limit int) (Page, error) {
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))
	q.Set("after", strconv.FormatInt(after, 10))

	var p Page

	history, err := c.get(ctx, network, "/api/v1/history?"+q.Encode(), &p.Events)
	if err != nil {
		return Page{}, err
	}

	p.History = history

	return p, nil
}

// Latest is where a network's history stands.
func (c *Client) Latest(ctx context.Context, network string) (Position, error) {
	var events []Event

	history, err := c.get(ctx, network, "/api/v1/history?limit=1", &events)
	if err != nil {
		return Position{}, err
	}

	pos := Position{History: history}
	if len(events) > 0 {
		pos.ID = events[0].ID
	}

	return pos, nil
}

// get decodes a JSON answer into out and returns the history identity the
// answer carried, if any.
func (c *Client) get(ctx context.Context, network, path string, out any) (string, error) {
	if !networkPattern.MatchString(network) {
		return "", fmt.Errorf("invalid network name %q", network)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL(network)+path, http.NoBody)
	if err != nil {
		return "", err
	}

	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("rolloor for %s: %w", network, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", fmt.Errorf("rolloor for %s: %w", network, err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("rolloor for %s answered %s", network, resp.Status)
	}

	if err := json.Unmarshal(body, out); err != nil {
		return "", fmt.Errorf("rolloor for %s: %w", network, err)
	}

	return resp.Header.Get(HistoryHeader), nil
}
