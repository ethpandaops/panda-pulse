package cartographoor

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ethpandaops/cartographoor/pkg/client"
	"github.com/ethpandaops/cartographoor/pkg/discovery"
	"github.com/ethpandaops/panda-pulse/pkg/clients"
	"github.com/sirupsen/logrus"
)

const (
	active                 = "active"
	devnet                 = "devnet"
	defaultRefreshInterval = 1 * time.Hour
	defaultRequestTimeout  = 30 * time.Second
)

// Service provides access to cartographoor data with automatic updates from a
// remote source. It wraps the official cartographoor client, layering on the
// devnet-only filtering and client-role lookups panda-pulse needs, while keeping
// a local snapshot so callers can query synchronously without a context.
type Service struct {
	log      *logrus.Logger
	provider client.Provider
	done     chan struct{}
	wg       sync.WaitGroup

	dataMu   sync.RWMutex
	networks map[string]discovery.Network
	clients  map[string]discovery.ClientInfo
}

// ServiceConfig contains the configuration for the cartographoor service.
type ServiceConfig struct {
	SourceURL       string
	RefreshInterval time.Duration
	Logger          *logrus.Logger
	HTTPClient      *http.Client
}

// NewService creates a new cartographoor service and performs the initial
// (blocking) data fetch. It returns an error if the initial fetch fails so the
// caller can fail fast at startup.
func NewService(ctx context.Context, config ServiceConfig) (*Service, error) {
	if config.Logger == nil {
		config.Logger = logrus.New()
	}

	if config.RefreshInterval == 0 {
		config.RefreshInterval = defaultRefreshInterval
	}

	// An empty SourceURL falls back to the client's default production endpoint,
	// which matches the URL panda-pulse used previously.
	provider, err := client.NewMemoryProvider(client.Config{
		SourceURL:       config.SourceURL,
		RefreshInterval: config.RefreshInterval,
		RequestTimeout:  defaultRequestTimeout,
		HTTPClient:      config.HTTPClient,
	}, config.Logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create cartographoor provider: %w", err)
	}

	// Initial (blocking) fetch plus the provider's own background refresh loop.
	if err := provider.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start cartographoor provider: %w", err)
	}

	return newService(ctx, config.Logger, provider)
}

// newService wraps an already-started provider and loads the initial snapshot.
// It is the injection seam used by tests to supply a controllable provider.
func newService(ctx context.Context, log *logrus.Logger, provider client.Provider) (*Service, error) {
	if log == nil {
		log = logrus.New()
	}

	s := &Service{
		log:      log,
		provider: provider,
		done:     make(chan struct{}),
		networks: make(map[string]discovery.Network),
		clients:  make(map[string]discovery.ClientInfo),
	}

	if err := s.rebuild(ctx); err != nil {
		return nil, fmt.Errorf("failed to load initial cartographoor data: %w", err)
	}

	return s, nil
}

// Start begins watching the provider for updates, refreshing the local snapshot
// whenever new data is fetched.
func (s *Service) Start(ctx context.Context) {
	s.wg.Go(func() {
		s.watch(ctx)
	})

	s.log.Info("Cartographoor service started")
}

// Stop halts the update watcher and the underlying provider.
func (s *Service) Stop() {
	close(s.done)
	s.wg.Wait()

	if err := s.provider.Stop(); err != nil {
		s.log.WithError(err).Warn("Error stopping cartographoor provider")
	}

	s.log.Info("Cartographoor service stopped")
}

// GetClientRepository returns the repository for a client.
func (s *Service) GetClientRepository(clientName string) string {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()

	if c, ok := s.clients[clientName]; ok {
		return c.Repository
	}

	return ""
}

// GetClientBranch returns the default branch for a client.
func (s *Service) GetClientBranch(clientName string) string {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()

	if c, ok := s.clients[clientName]; ok {
		return c.Branch
	}

	return ""
}

// GetClientLogo returns the logo URL for a client.
func (s *Service) GetClientLogo(clientName string) string {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()

	if c, ok := s.clients[clientName]; ok && c.Logo != "" {
		return c.Logo
	}

	return ""
}

// GetClientLatestVersion returns the latest version for a client.
func (s *Service) GetClientLatestVersion(clientName string) string {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()

	if c, ok := s.clients[clientName]; ok {
		return c.LatestVersion
	}

	return ""
}

// GetClientWebsiteURL returns the website URL for a client.
func (s *Service) GetClientWebsiteURL(clientName string) string {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()

	if c, ok := s.clients[clientName]; ok {
		return c.WebsiteURL
	}

	return ""
}

// GetClientDocsURL returns the documentation URL for a client.
func (s *Service) GetClientDocsURL(clientName string) string {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()

	if c, ok := s.clients[clientName]; ok {
		return c.DocsURL
	}

	return ""
}

// IsCLClient checks if a client is a consensus layer client.
func (s *Service) IsCLClient(clientName string) bool {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()

	if c, ok := s.clients[clientName]; ok {
		return c.Type == string(clients.ClientTypeCL)
	}

	return false
}

// IsELClient checks if a client is an execution layer client.
func (s *Service) IsELClient(clientName string) bool {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()

	if c, ok := s.clients[clientName]; ok {
		return c.Type == string(clients.ClientTypeEL)
	}

	return false
}

// GetClientDisplayName returns the display name for a client.
func (s *Service) GetClientDisplayName(clientName string) string {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()

	if c, ok := s.clients[clientName]; ok && c.DisplayName != "" {
		return c.DisplayName
	}

	return clientName
}

// GetClientType returns the type for a client (consensus or execution).
func (s *Service) GetClientType(clientName string) string {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()

	if c, ok := s.clients[clientName]; ok {
		return c.Type
	}

	return ""
}

// GetConsensusClients returns all consensus clients from the remote data.
func (s *Service) GetConsensusClients() []string {
	return s.clientsOfType(clients.ClientTypeCL)
}

// GetExecutionClients returns all execution clients from the remote data.
func (s *Service) GetExecutionClients() []string {
	return s.clientsOfType(clients.ClientTypeEL)
}

// GetAllClients returns all known client names.
func (s *Service) GetAllClients() []string {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()

	clientsList := make([]string, 0, len(s.clients))
	for name := range s.clients {
		clientsList = append(clientsList, name)
	}

	return clientsList
}

// GetActiveNetworks returns all active devnets sorted alphabetically.
func (s *Service) GetActiveNetworks() []string {
	return s.devnetsMatching(func(n discovery.Network) bool {
		return n.Status == active
	})
}

// GetInactiveNetworks returns all inactive devnets sorted alphabetically.
func (s *Service) GetInactiveNetworks() []string {
	return s.devnetsMatching(func(n discovery.Network) bool {
		return n.Status != active
	})
}

// GetAllNetworks returns all devnets regardless of status, sorted alphabetically.
func (s *Service) GetAllNetworks() []string {
	return s.devnetsMatching(func(discovery.Network) bool {
		return true
	})
}

// GetNetwork returns information about a specific devnet, or nil if the network
// is unknown or is not a devnet.
func (s *Service) GetNetwork(networkName string) *discovery.Network {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()

	if network, ok := s.networks[networkName]; ok && strings.Contains(networkName, devnet) {
		return &network
	}

	return nil
}

// GetNetworkStatus returns the status of a devnet, or an empty string if the
// network is unknown or is not a devnet.
func (s *Service) GetNetworkStatus(networkName string) string {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()

	if network, ok := s.networks[networkName]; ok && strings.Contains(networkName, devnet) {
		return network.Status
	}

	return ""
}

// GetTeamRoles returns the team roles for a client.
func (s *Service) GetTeamRoles(clientName string) []string {
	return clients.TeamRoles[clientName]
}

// IsPreProductionClient checks if a client is a pre-production client.
func (s *Service) IsPreProductionClient(clientName string) bool {
	return clients.PreProductionClients[clientName]
}

// GetCLClients returns all consensus layer client names.
func (s *Service) GetCLClients() []string {
	return s.GetConsensusClients()
}

// GetELClients returns all execution layer client names.
func (s *Service) GetELClients() []string {
	return s.GetExecutionClients()
}

// GetAdminRoles returns all admin roles.
func (s *Service) GetAdminRoles() map[string][]string {
	return clients.AdminRoles
}

// watch listens for provider update notifications and refreshes the local
// snapshot until the service is stopped or the context is cancelled.
func (s *Service) watch(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			s.log.Info("Cartographoor service context done")

			return
		case <-s.done:
			return
		case <-s.provider.NotifyChannel():
			if err := s.rebuild(ctx); err != nil {
				s.log.WithError(err).Error("Failed to refresh cartographoor data")
			}
		}
	}
}

// rebuild refreshes the local snapshot from the provider.
func (s *Service) rebuild(ctx context.Context) error {
	networks, err := s.provider.GetNetworks(ctx)
	if err != nil {
		return fmt.Errorf("get networks: %w", err)
	}

	clientList, err := s.provider.GetClients(ctx)
	if err != nil {
		return fmt.Errorf("get clients: %w", err)
	}

	s.dataMu.Lock()
	s.networks = networks
	s.clients = clientList
	s.dataMu.Unlock()

	var (
		activeDevnets   = 0
		inactiveDevnets = 0
	)

	for name, network := range networks {
		if !strings.Contains(name, devnet) {
			continue
		}

		if network.Status == active {
			activeDevnets++
		} else {
			inactiveDevnets++
		}
	}

	s.log.WithFields(logrus.Fields{
		"networks_count":   len(networks),
		"active_devnets":   activeDevnets,
		"inactive_devnets": inactiveDevnets,
		"clients_count":    len(clientList),
	}).Info("Cartographoor updated")

	return nil
}

// clientsOfType returns the names of all clients matching the given type.
func (s *Service) clientsOfType(clientType clients.ClientType) []string {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()

	clientsList := make([]string, 0, len(s.clients))

	for name, c := range s.clients {
		if c.Type == string(clientType) {
			clientsList = append(clientsList, name)
		}
	}

	return clientsList
}

// devnetsMatching returns the names of all devnets satisfying the predicate,
// sorted alphabetically.
func (s *Service) devnetsMatching(match func(discovery.Network) bool) []string {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()

	networks := make([]string, 0, len(s.networks))

	for key, network := range s.networks {
		if strings.Contains(key, devnet) && match(network) {
			networks = append(networks, key)
		}
	}

	sort.Strings(networks)

	return networks
}
