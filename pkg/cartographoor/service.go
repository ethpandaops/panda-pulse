package cartographoor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ethpandaops/panda-pulse/pkg/clients"
	"github.com/sirupsen/logrus"
)

const (
	defaultRefreshInterval = 1 * time.Hour
	defaultRequestTimeout  = 30 * time.Second
)

// Service provides access to cartographoor data with automatic updates from a remote source.
type Service struct {
	log           *logrus.Logger
	sourceURL     string
	refreshTicker *time.Ticker
	httpClient    *http.Client
	stopChan      chan struct{}
	dataMu        sync.RWMutex
	remoteData    *NetworksData
}

// NetworksData represents the structure of the networks.json file.
type NetworksData struct {
	NetworkMetadata map[string]NetworkMetadata `json:"networkMetadata,omitempty"`
	Networks        map[string]NetworkInfo     `json:"networks"`
	Clients         map[string]ClientData      `json:"clients"`
	LastUpdate      string                     `json:"lastUpdate,omitempty"`
	Duration        float64                    `json:"duration,omitempty"`
	Providers       []interface{}              `json:"providers,omitempty"`
}

// NetworkMetadata represents metadata about network types.
type NetworkMetadata struct {
	DisplayName string       `json:"displayName"`
	Description string       `json:"description"`
	Links       []Link       `json:"links"`
	Image       string       `json:"image"`
	Stats       NetworkStats `json:"stats"`
}

// Link represents a hyperlink with title and URL.
type Link struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// NetworkStats contains statistics about a network type.
type NetworkStats struct {
	TotalNetworks    int      `json:"totalNetworks"`
	ActiveNetworks   int      `json:"activeNetworks"`
	InactiveNetworks int      `json:"inactiveNetworks"`
	NetworkNames     []string `json:"networkNames"`
}

// NetworkInfo represents information about a specific network.
type NetworkInfo struct {
	Name          string        `json:"name"`
	Description   string        `json:"description,omitempty"`
	Repository    string        `json:"repository,omitempty"`
	Path          string        `json:"path,omitempty"`
	URL           string        `json:"url,omitempty"`
	Status        string        `json:"status"` // "active" or "inactive"
	LastUpdated   string        `json:"lastUpdated,omitempty"`
	ChainID       int64         `json:"chainId,omitempty"`
	GenesisConfig interface{}   `json:"genesisConfig,omitempty"`
	ServiceURLs   ServiceURLs   `json:"serviceUrls,omitempty"`
	Images        NetworkImages `json:"images,omitempty"`
}

// ClientData represents the structure of a client in the networks.json file.
type ClientData struct {
	Name          string `json:"name"`
	DisplayName   string `json:"displayName"`
	Repository    string `json:"repository"`
	Type          string `json:"type"`
	Branch        string `json:"branch"`
	Logo          string `json:"logo"`
	LatestVersion string `json:"latestVersion"`
	WebsiteURL    string `json:"websiteUrl"`
	DocsURL       string `json:"docsUrl"`
}

// ServiceURLs contains URLs to various services for a network.
type ServiceURLs struct {
	JSONRPC        string `json:"jsonRpc,omitempty"`
	BeaconRPC      string `json:"beaconRpc,omitempty"`
	Explorer       string `json:"explorer,omitempty"`
	BeaconExplorer string `json:"beaconExplorer,omitempty"`
	Forkmon        string `json:"forkmon,omitempty"`
	Assertoor      string `json:"assertoor,omitempty"`
	Dora           string `json:"dora,omitempty"`
	CheckpointSync string `json:"checkpointSync,omitempty"`
	Blobscan       string `json:"blobscan,omitempty"`
	BlobArchive    string `json:"blobArchive,omitempty"`
	Ethstats       string `json:"ethstats,omitempty"`
	DevnetSpec     string `json:"devnetSpec,omitempty"`
	Forky          string `json:"forky,omitempty"`
	Tracoor        string `json:"tracoor,omitempty"`
}

// NetworkImages contains image information for a network.
type NetworkImages struct {
	URL     string        `json:"url,omitempty"`
	Clients []ClientImage `json:"clients,omitempty"`
	Tools   []ToolImage   `json:"tools,omitempty"`
}

// ClientImage represents a client docker image.
type ClientImage struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ToolImage represents a tool docker image.
type ToolImage struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ServiceConfig contains the configuration for the cartographoor service.
type ServiceConfig struct {
	SourceURL       string
	RefreshInterval time.Duration
	Logger          *logrus.Logger
	HTTPClient      *http.Client
}

// NewService creates a new cartographoor service.
func NewService(ctx context.Context, config ServiceConfig) (*Service, error) {
	if config.SourceURL == "" {
		config.SourceURL = "https://ethpandaops-platform-production-cartographoor.ams3.cdn.digitaloceanspaces.com/networks.json"
	}

	if config.RefreshInterval == 0 {
		config.RefreshInterval = defaultRefreshInterval
	}

	if config.Logger == nil {
		config.Logger = logrus.New()
	}

	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: defaultRequestTimeout,
		}
	}

	service := &Service{
		log:           config.Logger,
		sourceURL:     config.SourceURL,
		refreshTicker: time.NewTicker(config.RefreshInterval),
		httpClient:    httpClient,
		stopChan:      make(chan struct{}),
	}

	// Perform initial fetch
	if err := service.fetchAndUpdateData(ctx); err != nil {
		return nil, fmt.Errorf("initial data fetch failed: %w", err)
	}

	return service, nil
}

// Start begins the periodic refresh of cartographoor data.
func (s *Service) Start(ctx context.Context) {
	go func() {
		for {
			select {
			case <-s.refreshTicker.C:
				if err := s.fetchAndUpdateData(ctx); err != nil {
					s.log.WithError(err).Error("Failed to refresh cartographoor data")
				}
			case <-s.stopChan:
				s.log.Info("Cartographoor service stopped")

				return
			case <-ctx.Done():
				s.log.Info("Cartographoor service context done")

				return
			}
		}
	}()

	s.log.Info("Cartographoor service started")
}

// Stop halts the periodic refresh of cartographoor data.
func (s *Service) Stop() {
	s.refreshTicker.Stop()

	close(s.stopChan)
}

// fetchAndUpdateData retrieves the latest data from the remote source.
func (s *Service) fetchAndUpdateData(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, defaultRequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.sourceURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var data NetworksData
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return fmt.Errorf("failed to decode data: %w", err)
	}

	s.dataMu.Lock()
	s.remoteData = &data
	s.dataMu.Unlock()

	// Count statistics for logging
	var (
		activeNetworksCount   = 0
		inactiveNetworksCount = 0
		consensusClientsCount = 0
		executionClientsCount = 0
		unknownClientsCount   = 0
	)

	for _, network := range data.Networks {
		// We only want devnets, so make sure the name contains "devnet".
		if network.Status == "active" && strings.Contains(network.Name, "devnet") {
			activeNetworksCount++
		} else {
			inactiveNetworksCount++
		}
	}

	for _, client := range data.Clients {
		switch client.Type {
		case string(clients.ClientTypeCL):
			consensusClientsCount++
		case string(clients.ClientTypeEL):
			executionClientsCount++
		default:
			unknownClientsCount++
		}
	}

	s.log.WithFields(logrus.Fields{
		"networks_count":    len(data.Networks),
		"active_networks":   activeNetworksCount,
		"inactive_networks": inactiveNetworksCount,
		"clients_count":     len(data.Clients),
		"consensus_clients": consensusClientsCount,
		"execution_clients": executionClientsCount,
		"unknown_type":      unknownClientsCount,
	}).Info("Cartographoor updated")

	return nil
}

// GetClientRepository returns the repository for a client.
func (s *Service) GetClientRepository(clientName string) string {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()

	if s.remoteData == nil {
		return ""
	}

	if client, ok := s.remoteData.Clients[clientName]; ok {
		return client.Repository
	}

	return ""
}

// GetClientBranch returns the default branch for a client.
func (s *Service) GetClientBranch(clientName string) string {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()

	if s.remoteData == nil {
		return ""
	}

	if client, ok := s.remoteData.Clients[clientName]; ok {
		return client.Branch
	}

	return ""
}

// GetClientLogo returns the logo URL for a client.
func (s *Service) GetClientLogo(clientName string) string {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()

	if s.remoteData == nil {
		return ""
	}

	if client, ok := s.remoteData.Clients[clientName]; ok && client.Logo != "" {
		return client.Logo
	}

	return ""
}

// GetClientLatestVersion returns the latest version for a client.
func (s *Service) GetClientLatestVersion(clientName string) string {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()

	if s.remoteData == nil {
		return ""
	}

	if client, ok := s.remoteData.Clients[clientName]; ok {
		return client.LatestVersion
	}

	return ""
}

// GetClientWebsiteURL returns the website URL for a client.
func (s *Service) GetClientWebsiteURL(clientName string) string {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()

	if s.remoteData == nil {
		return ""
	}

	if client, ok := s.remoteData.Clients[clientName]; ok {
		return client.WebsiteURL
	}

	return ""
}

// GetClientDocsURL returns the documentation URL for a client.
func (s *Service) GetClientDocsURL(clientName string) string {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()

	if s.remoteData == nil {
		return ""
	}

	if client, ok := s.remoteData.Clients[clientName]; ok {
		return client.DocsURL
	}

	return ""
}

// IsCLClient checks if a client is a consensus layer client.
func (s *Service) IsCLClient(clientName string) bool {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()

	if s.remoteData == nil {
		return false
	}

	if client, ok := s.remoteData.Clients[clientName]; ok {
		return client.Type == string(clients.ClientTypeCL)
	}

	return false
}

// IsELClient checks if a client is an execution layer client.
func (s *Service) IsELClient(clientName string) bool {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()

	if s.remoteData == nil {
		return false
	}

	if client, ok := s.remoteData.Clients[clientName]; ok {
		return client.Type == string(clients.ClientTypeEL)
	}

	return false
}

// GetClientDisplayName returns the display name for a client.
func (s *Service) GetClientDisplayName(clientName string) string {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()

	if s.remoteData == nil {
		return clientName
	}

	if client, ok := s.remoteData.Clients[clientName]; ok && client.DisplayName != "" {
		return client.DisplayName
	}

	return clientName
}

// GetClientType returns the type for a client (consensus or execution).
func (s *Service) GetClientType(clientName string) string {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()

	if s.remoteData == nil {
		return ""
	}

	if client, ok := s.remoteData.Clients[clientName]; ok {
		return client.Type
	}

	return ""
}

// GetConsensusClients returns all consensus clients from the remote data.
func (s *Service) GetConsensusClients() []string {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()

	if s.remoteData == nil {
		return []string{}
	}

	clientsList := make([]string, 0)

	for name, client := range s.remoteData.Clients {
		if client.Type == string(clients.ClientTypeCL) {
			clientsList = append(clientsList, name)
		}
	}

	return clientsList
}

// GetExecutionClients returns all execution clients from the remote data.
func (s *Service) GetExecutionClients() []string {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()

	if s.remoteData == nil {
		return []string{}
	}

	clientsList := make([]string, 0)

	for name, client := range s.remoteData.Clients {
		if client.Type == string(clients.ClientTypeEL) {
			clientsList = append(clientsList, name)
		}
	}

	return clientsList
}

// GetAllClients returns all known client names.
func (s *Service) GetAllClients() []string {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()

	if s.remoteData == nil {
		return []string{}
	}

	clientsList := make([]string, 0, len(s.remoteData.Clients))
	for name := range s.remoteData.Clients {
		clientsList = append(clientsList, name)
	}

	return clientsList
}

// GetActiveNetworks returns all active networks sorted alphabetically.
func (s *Service) GetActiveNetworks() []string {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()

	if s.remoteData == nil {
		return []string{}
	}

	networks := make([]string, 0)

	for key, network := range s.remoteData.Networks {
		if network.Status == "active" && strings.Contains(key, "devnet") {
			networks = append(networks, key)
		}
	}

	sort.Strings(networks)

	return networks
}

// GetInactiveNetworks returns all inactive networks sorted alphabetically.
func (s *Service) GetInactiveNetworks() []string {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()

	if s.remoteData == nil {
		return []string{}
	}

	networks := make([]string, 0)

	for key, network := range s.remoteData.Networks {
		if network.Status != "active" && strings.Contains(key, "devnet") {
			networks = append(networks, key)
		}
	}

	sort.Strings(networks)

	return networks
}

// GetAllNetworks returns all networks regardless of status.
func (s *Service) GetAllNetworks() []string {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()

	if s.remoteData == nil {
		return []string{}
	}

	networks := make([]string, 0, len(s.remoteData.Networks))

	for key := range s.remoteData.Networks {
		if strings.Contains(key, "devnet") {
			networks = append(networks, key)
		}
	}

	return networks
}

// GetNetwork returns information about a specific network.
func (s *Service) GetNetwork(networkName string) *NetworkInfo {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()

	if s.remoteData == nil {
		return nil
	}

	if network, ok := s.remoteData.Networks[networkName]; ok {
		if strings.Contains(networkName, "devnet") {
			return &network
		}
	}

	return nil
}

// GetNetworkStatus returns the status of a network.
func (s *Service) GetNetworkStatus(networkName string) string {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()

	if s.remoteData == nil {
		return ""
	}

	if network, ok := s.remoteData.Networks[networkName]; ok {
		if strings.Contains(networkName, "devnet") {
			return network.Status
		}
	}

	return ""
}

// GetNetworksOfType returns all networks of a specific type.
func (s *Service) GetNetworksOfType(networkType string) []string {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()

	if s.remoteData == nil {
		return []string{}
	}

	if metadata, ok := s.remoteData.NetworkMetadata[networkType]; ok {
		prefix := networkType + "-"
		networks := make([]string, 0, len(metadata.Stats.NetworkNames))

		for _, name := range metadata.Stats.NetworkNames {
			networks = append(networks, prefix+name)
		}

		return networks
	}

	return []string{}
}

// GetTeamRole returns the team role for a client.
func (s *Service) GetTeamRole(clientName string) string {
	return clients.TeamRoles[clientName]
}

// IsPreProductionClient checks if a client is a pre-production client.
func (s *Service) IsPreProductionClient(clientName string) bool {
	return clients.PreProductionClients[clientName]
}

// ClientSupportsBuildArgs checks if a client supports build arguments.
func (s *Service) ClientSupportsBuildArgs(clientName string) bool {
	if clientInfo, exists := clients.ClientsWithBuildArgs[clientName]; exists {
		return clientInfo.HasBuildArgs
	}

	return false
}

// GetClientDefaultBuildArgs returns the default build arguments for a client.
func (s *Service) GetClientDefaultBuildArgs(clientName string) string {
	if clientInfo, exists := clients.ClientsWithBuildArgs[clientName]; exists && clientInfo.BuildArgs != "" {
		return clientInfo.BuildArgs
	}

	return ""
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
func (s *Service) GetAdminRoles() map[string]string {
	return clients.AdminRoles
}
