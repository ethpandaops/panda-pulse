package rolloor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fakeRolloor(t *testing.T, probes *atomic.Int32) *Client {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /n/on-devnet-1/healthz", func(w http.ResponseWriter, _ *http.Request) {
		probes.Add(1)
		_, _ = w.Write([]byte(`{"ok":true,"environment":"on-devnet-1"}`))
	})
	mux.HandleFunc("GET /n/dora-devnet-1/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<html>not rolloor</html>`))
	})
	mux.HandleFunc("GET /n/slow-devnet-1/healthz", func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	mux.HandleFunc("GET /n/on-devnet-1/api/v1/history", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("after") == "10" {
			_, _ = w.Write([]byte(`[{"id":11,"action":"rollout.created","group":"lighthouse"},{"id":12,"action":"batch.started"}]`))

			return
		}

		_, _ = w.Write([]byte(`[{"id":12,"action":"batch.started"}]`))
	})
	mux.HandleFunc("GET /n/empty-devnet-1/api/v1/history", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})
	mux.HandleFunc("GET /n/broken-devnet-1/api/v1/history", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{`))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return NewClientWithPattern(srv.Client(), srv.URL+"/n/%s")
}

func TestAvailableProbesOnceAndOnlyTrustsRolloor(t *testing.T) {
	var probes atomic.Int32

	c := fakeRolloor(t, &probes)
	ctx := context.Background()

	assert.True(t, c.Available(ctx, "on-devnet-1"))
	assert.True(t, c.Available(ctx, "on-devnet-1"))
	assert.Equal(t, int32(1), probes.Load(), "answers are remembered")

	c.now = func() time.Time { return time.Now().Add(probeTTL + time.Second) }
	assert.True(t, c.Available(ctx, "on-devnet-1"))
	assert.Equal(t, int32(2), probes.Load(), "and asked again once stale")

	assert.False(t, c.Available(ctx, "dora-devnet-1"), "something else answering is not rolloor")
	assert.False(t, c.Available(ctx, "missing-devnet-1"))
	assert.False(t, c.Available(ctx, "Bad_Name"))

	// A probe cut short by the caller is not remembered as "no rolloor".
	short, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	assert.False(t, c.Available(short, "slow-devnet-1"))

	c.mu.Lock()
	_, remembered := c.probes["slow-devnet-1"]
	c.mu.Unlock()
	assert.False(t, remembered)

	assert.Equal(t, "https://rolloor.x-devnet-1.ethpandaops.io", NewClient(http.DefaultClient).URL("x-devnet-1"))
}

func TestHistoryAndLatest(t *testing.T) {
	var probes atomic.Int32

	c := fakeRolloor(t, &probes)
	ctx := context.Background()

	events, err := c.History(ctx, "on-devnet-1", 10, 200)
	require.NoError(t, err)
	require.Len(t, events, 2)
	assert.Equal(t, "rollout.created", events[0].Action)

	latest, err := c.Latest(ctx, "on-devnet-1")
	require.NoError(t, err)
	assert.Equal(t, int64(12), latest)

	latest, err = c.Latest(ctx, "empty-devnet-1")
	require.NoError(t, err)
	assert.Zero(t, latest)

	_, err = c.History(ctx, "broken-devnet-1", 0, 1)
	require.Error(t, err)

	_, err = c.History(ctx, "missing-devnet-1", 0, 1)
	require.ErrorContains(t, err, "404")

	_, err = c.History(ctx, "Bad_Name", 0, 1)
	require.ErrorContains(t, err, "invalid network")

	unreachable := NewClientWithPattern(http.DefaultClient, "http://127.0.0.1:1/%s")
	_, err = unreachable.History(ctx, "on-devnet-1", 0, 1)
	require.Error(t, err)
}
