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

	if err := c.get(ctx, network, "/healthz", &health); err != nil {
		return false
	}

	return health.OK && health.Environment != ""
}

// History returns the events after an id, oldest first. After zero returns
// the newest events instead, newest first.
func (c *Client) History(ctx context.Context, network string, after int64, limit int) ([]Event, error) {
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))

	if after > 0 {
		q.Set("after", strconv.FormatInt(after, 10))
	}

	var events []Event
	if err := c.get(ctx, network, "/api/v1/history?"+q.Encode(), &events); err != nil {
		return nil, err
	}

	return events, nil
}

// Latest is the id of a network's newest event, or zero when it has none.
func (c *Client) Latest(ctx context.Context, network string) (int64, error) {
	events, err := c.History(ctx, network, 0, 1)
	if err != nil || len(events) == 0 {
		return 0, err
	}

	return events[0].ID, nil
}

func (c *Client) get(ctx context.Context, network, path string, out any) error {
	if !networkPattern.MatchString(network) {
		return fmt.Errorf("invalid network name %q", network)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL(network)+path, http.NoBody)
	if err != nil {
		return err
	}

	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("rolloor for %s: %w", network, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("rolloor for %s: %w", network, err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("rolloor for %s answered %s", network, resp.Status)
	}

	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("rolloor for %s: %w", network, err)
	}

	return nil
}
