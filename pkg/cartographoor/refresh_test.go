package cartographoor

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ethpandaops/cartographoor/pkg/client"
	"github.com/ethpandaops/cartographoor/pkg/discovery"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

// fakeProvider is a controllable client.Provider for exercising the Service's
// refresh path without a real HTTP source or ticker.
type fakeProvider struct {
	mu       sync.RWMutex
	networks map[string]discovery.Network
	clients  map[string]discovery.ClientInfo
	notifyCh chan struct{}
}

// Compile-time interface check.
var _ client.Provider = (*fakeProvider)(nil)

func newFakeProvider() *fakeProvider {
	return &fakeProvider{
		networks: make(map[string]discovery.Network),
		clients:  make(map[string]discovery.ClientInfo),
		notifyCh: make(chan struct{}, 1),
	}
}

func (f *fakeProvider) setNetworks(networks map[string]discovery.Network) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.networks = networks
}

func (f *fakeProvider) notify() {
	select {
	case f.notifyCh <- struct{}{}:
	default:
	}
}

func (f *fakeProvider) Start(context.Context) error { return nil }
func (f *fakeProvider) Stop() error                 { return nil }
func (f *fakeProvider) Ready() bool                 { return true }
func (f *fakeProvider) NotifyChannel() <-chan struct{} {
	return f.notifyCh
}

func (f *fakeProvider) GetNetworks(context.Context) (map[string]discovery.Network, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	out := make(map[string]discovery.Network, len(f.networks))
	for k, v := range f.networks {
		out[k] = v
	}

	return out, nil
}

func (f *fakeProvider) GetClients(context.Context) (map[string]discovery.ClientInfo, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	out := make(map[string]discovery.ClientInfo, len(f.clients))
	for k, v := range f.clients {
		out[k] = v
	}

	return out, nil
}

func (f *fakeProvider) GetNetwork(context.Context, string) (discovery.Network, bool, error) {
	return discovery.Network{}, false, nil
}

func (f *fakeProvider) GetActiveNetworks(context.Context) (map[string]discovery.Network, error) {
	return map[string]discovery.Network{}, nil
}

func (f *fakeProvider) GetInactiveNetworks(context.Context) (map[string]discovery.Network, error) {
	return map[string]discovery.Network{}, nil
}

func (f *fakeProvider) GetNetworksByStatus(context.Context, string) (map[string]discovery.Network, error) {
	return map[string]discovery.Network{}, nil
}

func (f *fakeProvider) GetClient(context.Context, string) (discovery.ClientInfo, bool, error) {
	return discovery.ClientInfo{}, false, nil
}

func (f *fakeProvider) GetClientsByType(context.Context, string) (map[string]discovery.ClientInfo, error) {
	return map[string]discovery.ClientInfo{}, nil
}

// TestServiceRefresh verifies the Service updates its local snapshot when the
// provider signals new data on its notify channel.
func TestServiceRefresh(t *testing.T) {
	ctx := context.Background()

	fp := newFakeProvider()
	fp.setNetworks(map[string]discovery.Network{
		"foo-devnet-0": {Name: "devnet-0", Status: active},
	})

	svc, err := newService(ctx, logrus.New(), fp)
	require.NoError(t, err)

	svc.Start(ctx)
	defer svc.Stop()

	// Initial snapshot loaded by newService.
	require.Equal(t, []string{"foo-devnet-0"}, svc.GetActiveNetworks())

	// A refresh brings in a second active devnet and flips the first to inactive.
	fp.setNetworks(map[string]discovery.Network{
		"foo-devnet-0": {Name: "devnet-0", Status: "inactive"},
		"bar-devnet-1": {Name: "devnet-1", Status: active},
	})
	fp.notify()

	require.Eventually(t, func() bool {
		active := svc.GetActiveNetworks()

		return len(active) == 1 && active[0] == "bar-devnet-1"
	}, 2*time.Second, 10*time.Millisecond, "snapshot should reflect provider refresh")

	require.Equal(t, []string{"foo-devnet-0"}, svc.GetInactiveNetworks())
}
