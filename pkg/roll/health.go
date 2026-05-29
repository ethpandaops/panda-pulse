package roll

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// BeaconHealth checks a beacon node's sync status for rollout gating.
type BeaconHealth struct {
	httpClient *http.Client
	user       string
	pass       string
}

// NewBeaconHealth returns a BeaconHealth checker.
func NewBeaconHealth() *BeaconHealth {
	return &BeaconHealth{httpClient: &http.Client{Timeout: 10 * time.Second}}
}

// SetBasicAuth sets HTTP basic auth for beacon requests, needed when the beacon
// endpoint is behind nginx basic auth (as the bn-* vhosts are).
func (b *BeaconHealth) SetBasicAuth(user, pass string) {
	b.user = user
	b.pass = pass
}

//nolint:tagliatelle // beacon API uses snake_case
type syncingResponse struct {
	Data struct {
		HeadSlot     string `json:"head_slot"`
		SyncDistance string `json:"sync_distance"`
		IsSyncing    bool   `json:"is_syncing"`
		IsOptimistic bool   `json:"is_optimistic"`
		ELOffline    bool   `json:"el_offline"`
	} `json:"data"`
}

// Healthy reports whether the beacon at beaconURL is synced within
// maxSyncDistance slots and not syncing/optimistic/EL-offline. The returned
// string is a human-readable status.
func (b *BeaconHealth) Healthy(ctx context.Context, beaconURL string, maxSyncDistance uint64) (bool, string, error) {
	url := strings.TrimRight(beaconURL, "/") + "/eth/v1/node/syncing"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, "", err
	}

	if b.user != "" || b.pass != "" {
		req.SetBasicAuth(b.user, b.pass)
	}

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return false, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, "", fmt.Errorf("beacon /syncing returned %d", resp.StatusCode)
	}

	var body syncingResponse
	if decErr := json.NewDecoder(resp.Body).Decode(&body); decErr != nil {
		return false, "", fmt.Errorf("decode /syncing: %w", decErr)
	}

	dist, err := strconv.ParseUint(body.Data.SyncDistance, 10, 64)
	if err != nil {
		return false, "", fmt.Errorf("parse sync_distance %q: %w", body.Data.SyncDistance, err)
	}

	switch {
	case body.Data.IsSyncing:
		return false, fmt.Sprintf("syncing (distance=%d)", dist), nil
	case body.Data.IsOptimistic:
		return false, "optimistic (execution payload not validated)", nil
	case body.Data.ELOffline:
		return false, "execution layer offline", nil
	case dist > maxSyncDistance:
		return false, fmt.Sprintf("sync distance %d exceeds max %d", dist, maxSyncDistance), nil
	}

	return true, fmt.Sprintf("synced (head=%s, distance=%d)", body.Data.HeadSlot, dist), nil
}
