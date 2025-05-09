package clients

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	defaultRefreshInterval = 1 * time.Hour
	defaultRequestTimeout  = 30 * time.Second
)

// Service provides access to client data with automatic updates from a remote source.
type Service struct {
	log           *logrus.Logger
	sourceURL     string
	refreshTicker *time.Ticker
	httpClient    *http.Client
	stopChan      chan struct{}
	clientsMu     sync.RWMutex
	remoteData    *NetworksData
}

// NetworksData represents the structure of the networks.json file.
type NetworksData struct {
	Clients map[string]ClientData `json:"clients"`
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

// ServiceConfig contains the configuration for the clients service.
type ServiceConfig struct {
	SourceURL       string
	RefreshInterval time.Duration
	Logger          *logrus.Logger
	HTTPClient      *http.Client
}

// NewService creates a new clients service.
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

// Start begins the periodic refresh of client data.
func (s *Service) Start(ctx context.Context) {
	go func() {
		for {
			select {
			case <-s.refreshTicker.C:
				if err := s.fetchAndUpdateData(ctx); err != nil {
					s.log.WithError(err).Error("Failed to refresh client data")
				}
			case <-s.stopChan:
				s.log.Info("Client service stopped")

				return
			case <-ctx.Done():
				s.log.Info("Client service context done")

				return
			}
		}
	}()

	s.log.Info("Client service started")
}

// Stop halts the periodic refresh of client data.
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

	s.clientsMu.Lock()
	s.remoteData = &data
	s.clientsMu.Unlock()

	// Count client types for logging
	var (
		consensusCount = 0
		executionCount = 0
		unknownCount   = 0
	)

	for _, client := range data.Clients {
		switch client.Type {
		case string(ClientTypeCL):
			consensusCount++
		case string(ClientTypeEL):
			executionCount++
		default:
			unknownCount++
		}
	}

	s.log.WithFields(logrus.Fields{
		"clients_count":     len(data.Clients),
		"consensus_clients": consensusCount,
		"execution_clients": executionCount,
		"unknown_type":      unknownCount,
	}).Info("Client data updated successfully")

	return nil
}

// GetClientRepository returns the repository for a client.
func (s *Service) GetClientRepository(clientName string) string {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

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
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

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
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

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
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

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
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

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
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

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
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	if s.remoteData == nil {
		return false
	}

	if client, ok := s.remoteData.Clients[clientName]; ok {
		return client.Type == string(ClientTypeCL)
	}

	return false
}

// IsELClient checks if a client is an execution layer client.
func (s *Service) IsELClient(clientName string) bool {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	if s.remoteData == nil {
		return false
	}

	if client, ok := s.remoteData.Clients[clientName]; ok {
		return client.Type == string(ClientTypeEL)
	}

	return false
}

// GetClientDisplayName returns the display name for a client.
func (s *Service) GetClientDisplayName(clientName string) string {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

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
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

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
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	if s.remoteData == nil {
		return []string{}
	}

	clients := make([]string, 0)

	for name, client := range s.remoteData.Clients {
		if client.Type == string(ClientTypeCL) {
			clients = append(clients, name)
		}
	}

	return clients
}

// GetExecutionClients returns all execution clients from the remote data.
func (s *Service) GetExecutionClients() []string {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	if s.remoteData == nil {
		return []string{}
	}

	clients := make([]string, 0)

	for name, client := range s.remoteData.Clients {
		if client.Type == string(ClientTypeEL) {
			clients = append(clients, name)
		}
	}

	return clients
}

// GetTeamRole returns the team role for a client.
func (s *Service) GetTeamRole(clientName string) string {
	return TeamRoles[clientName]
}

// IsPreProductionClient checks if a client is a pre-production client.
func (s *Service) IsPreProductionClient(clientName string) bool {
	return PreProductionClients[clientName]
}

// ClientSupportsBuildArgs checks if a client supports build arguments.
func (s *Service) ClientSupportsBuildArgs(clientName string) bool {
	if clientInfo, exists := ClientsWithBuildArgs[clientName]; exists {
		return clientInfo.HasBuildArgs
	}

	return false
}

// GetClientDefaultBuildArgs returns the default build arguments for a client.
func (s *Service) GetClientDefaultBuildArgs(clientName string) string {
	if clientInfo, exists := ClientsWithBuildArgs[clientName]; exists && clientInfo.BuildArgs != "" {
		return clientInfo.BuildArgs
	}

	return ""
}

// GetAllClients returns all known client names.
func (s *Service) GetAllClients() []string {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	if s.remoteData == nil {
		return []string{}
	}

	clients := make([]string, 0, len(s.remoteData.Clients))
	for name := range s.remoteData.Clients {
		clients = append(clients, name)
	}

	return clients
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
	return AdminRoles
}
