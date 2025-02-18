package service

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/ethpandaops/panda-pulse/pkg/checks"
	"github.com/ethpandaops/panda-pulse/pkg/discord"
	"github.com/ethpandaops/panda-pulse/pkg/grafana"
	"github.com/ethpandaops/panda-pulse/pkg/scheduler"
	"github.com/ethpandaops/panda-pulse/pkg/store"
)

type Service struct {
	config    *Config
	scheduler *scheduler.Scheduler
	bot       *discord.Bot
	grafana   grafana.Client
	checks    checks.Runner
	store     *store.S3Store
}

type Config struct {
	Network          string
	ConsensusNode    string
	ExecutionNode    string
	DiscordChannel   string
	GrafanaToken     string
	DiscordToken     string
	OpenRouterKey    string
	GrafanaBaseURL   string
	PromDatasourceID string
	AlertUnexplained bool
	Store            *store.S3Store
}

func NewService(cfg *Config) (*Service, error) {
	httpClient := &http.Client{Timeout: 30 * time.Second}

	grafanaClient := grafana.NewClient(
		cfg.GrafanaBaseURL,
		cfg.PromDatasourceID,
		cfg.GrafanaToken,
		httpClient,
	)

	svc := &Service{
		config:    cfg,
		scheduler: scheduler.NewScheduler(),
		grafana:   grafanaClient,
		checks:    checks.NewDefaultRunner(),
		store:     cfg.Store,
	}

	// Register checks
	svc.checks.RegisterCheck(checks.NewCLSyncCheck(grafanaClient))
	svc.checks.RegisterCheck(checks.NewHeadSlotCheck(grafanaClient))
	svc.checks.RegisterCheck(checks.NewCLFinalizedEpochCheck(grafanaClient))
	svc.checks.RegisterCheck(checks.NewELSyncCheck(grafanaClient))
	svc.checks.RegisterCheck(checks.NewELBlockHeightCheck(grafanaClient))

	bot, err := discord.NewBot(cfg.DiscordToken, cfg.Store, svc, grafanaClient, cfg.OpenRouterKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create discord bot: %w", err)
	}
	svc.bot = bot

	return svc, nil
}

func (s *Service) Start() error {
	log.Printf("Starting Discord bot...")
	if err := s.bot.Start(); err != nil {
		return fmt.Errorf("failed to start discord bot: %w", err)
	}

	log.Printf("Starting service, loading existing alerts...")
	alerts, err := s.store.ListNetworkAlerts(context.Background())
	if err != nil {
		return fmt.Errorf("failed to list alerts: %w", err)
	}
	log.Printf("Found %d existing alerts", len(alerts))

	for _, alert := range alerts {
		log.Printf("Scheduling existing alert: network=%s channel=%s", alert.Network, alert.DiscordChannel)
		if err := s.scheduleAlert(alert); err != nil {
			return fmt.Errorf("failed to schedule alert for %s: %w", alert.Network, err)
		}
	}

	log.Printf("Starting scheduler...")
	s.scheduler.Start()

	return nil
}

func (s *Service) RegisterAlert(ctx context.Context, network, channelID string, specificClient *string) error {
	if specificClient == nil {
		// For registering all clients, just proceed with registration
		return s.registerAllClients(ctx, network, channelID)
	}

	// Check if this specific client is already registered
	alerts, err := s.store.ListNetworkAlerts(ctx)
	if err != nil {
		return fmt.Errorf("failed to list alerts: %w", err)
	}

	for _, alert := range alerts {
		if alert.Network == network && alert.Client == *specificClient && alert.DiscordChannel == channelID {
			return &store.AlertAlreadyRegisteredError{
				Network: network,
				Channel: channelID,
				Client:  *specificClient,
			}
		}
	}

	// Check if client exists in our known clients
	clientType := checks.ClientTypeAll
	for _, client := range checks.CLClients {
		if client == *specificClient {
			clientType = checks.ClientTypeCL
			break
		}
	}

	if clientType == checks.ClientTypeAll {
		for _, client := range checks.ELClients {
			if client == *specificClient {
				clientType = checks.ClientTypeEL
				break
			}
		}
	}
	if clientType == checks.ClientTypeAll {
		return fmt.Errorf("unknown client: %s", *specificClient)
	}

	alert := &store.NetworkAlert{
		Network:        network,
		Client:         *specificClient,
		ClientType:     clientType,
		DiscordChannel: channelID,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	if err := s.store.RegisterNetworkAlert(ctx, alert); err != nil {
		return fmt.Errorf("failed to store alert: %w", err)
	}
	if err := s.scheduleAlert(alert); err != nil {
		return fmt.Errorf("failed to schedule alert: %w", err)
	}
	return nil
}

func (s *Service) registerAllClients(ctx context.Context, network, channelID string) error {
	// Register CL clients
	for _, client := range checks.CLClients {
		alert := &store.NetworkAlert{
			Network:        network,
			Client:         client,
			ClientType:     checks.ClientTypeCL,
			DiscordChannel: channelID,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		if err := s.store.RegisterNetworkAlert(ctx, alert); err != nil {
			return fmt.Errorf("failed to store CL alert: %w", err)
		}
		if err := s.scheduleAlert(alert); err != nil {
			return fmt.Errorf("failed to schedule CL alert: %w", err)
		}
	}

	// Register EL clients
	for _, client := range checks.ELClients {
		alert := &store.NetworkAlert{
			Network:        network,
			Client:         client,
			ClientType:     checks.ClientTypeEL,
			DiscordChannel: channelID,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		if err := s.store.RegisterNetworkAlert(ctx, alert); err != nil {
			return fmt.Errorf("failed to store EL alert: %w", err)
		}
		if err := s.scheduleAlert(alert); err != nil {
			return fmt.Errorf("failed to schedule EL alert: %w", err)
		}
	}
	return nil
}

func (s *Service) scheduleAlert(alert *store.NetworkAlert) error {
	schedule := "*/1 * * * *"
	jobName := fmt.Sprintf("network-health-%s-%s-%s", alert.Network, alert.ClientType, alert.Client)

	log.Printf("Scheduling alert: network=%s client=%s type=%s job=%s schedule=%s",
		alert.Network, alert.Client, alert.ClientType, jobName, schedule)

	return s.scheduler.AddJob(jobName, schedule, func(ctx context.Context) error {
		log.Printf("Running checks for network=%s client=%s", alert.Network, alert.Client)
		return s.runChecks(ctx, alert)
	})
}

func (s *Service) Stop() {
	log.Printf("Stopping service...")
	s.scheduler.Stop()
	if err := s.bot.Stop(); err != nil {
		log.Printf("Error stopping discord bot: %v", err)
	}
}

func (s *Service) runChecks(ctx context.Context, alert *store.NetworkAlert) error {
	var consensusNode, executionNode string
	if alert.ClientType == checks.ClientTypeCL {
		consensusNode = alert.Client
	} else {
		executionNode = alert.Client
	}

	results, analysis, err := s.checks.RunChecks(ctx, checks.Config{
		Network:       alert.Network,
		ConsensusNode: consensusNode,
		ExecutionNode: executionNode,
		GrafanaToken:  s.config.GrafanaToken,
	})
	if err != nil {
		return fmt.Errorf("failed to run checks: %w", err)
	}

	if _, err := s.bot.SendResults(
		alert.DiscordChannel,
		alert.Network,
		alert.Client,
		results,
		analysis,
		true, //s.config.AlertUnexplained,
	); err != nil {
		return fmt.Errorf("failed to send discord notification: %w", err)
	}

	return nil
}

func (s *Service) DeregisterAlert(ctx context.Context, network string, client *string) error {
	log.Printf("Deregistering alert for network=%s client=%v", network, client)

	// If client is specified, only remove that client's alert
	if client != nil {
		// First try to find the alert to get its type
		alerts, err := s.store.ListNetworkAlerts(ctx)
		if err != nil {
			return fmt.Errorf("failed to list alerts: %w", err)
		}

		// Find the alert to get its type
		var clientType checks.ClientType
		found := false
		for _, a := range alerts {
			if a.Network == network && a.Client == *client {
				clientType = a.ClientType
				found = true
				break
			}
		}

		if !found {
			return &store.AlertNotRegisteredError{
				Network: network,
				Client:  *client,
			}
		}

		jobName := fmt.Sprintf("network-health-%s-%s-%s", network, clientType, *client)
		s.scheduler.RemoveJob(jobName)

		// Remove from S3
		if err := s.store.DeleteNetworkAlert(ctx, network, *client); err != nil {
			return fmt.Errorf("failed to delete alert: %w", err)
		}
		return nil
	}

	// Otherwise, remove all clients for this network
	alerts, err := s.store.ListNetworkAlerts(ctx)
	if err != nil {
		return fmt.Errorf("failed to list alerts: %w", err)
	}

	found := false
	for _, alert := range alerts {
		if alert.Network == network {
			found = true
			jobName := fmt.Sprintf("network-health-%s-%s-%s", network, alert.ClientType, alert.Client)
			s.scheduler.RemoveJob(jobName)

			if err := s.store.DeleteNetworkAlert(ctx, network, alert.Client); err != nil {
				return fmt.Errorf("failed to delete alert: %w", err)
			}
		}
	}

	if !found {
		return &store.AlertNotRegisteredError{
			Network: network,
			Client:  "any",
		}
	}

	return nil
}

func (s *Service) ListAlerts(ctx context.Context, network *string) ([]*store.NetworkAlert, error) {
	alerts, err := s.store.ListNetworkAlerts(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list alerts: %w", err)
	}

	if network == nil {
		return alerts, nil
	}

	// Filter alerts for specific network
	filtered := make([]*store.NetworkAlert, 0)
	for _, alert := range alerts {
		if alert.Network == *network {
			filtered = append(filtered, alert)
		}
	}

	return filtered, nil
}

func (s *Service) RunChecks(ctx context.Context, alert *store.NetworkAlert) (bool, error) {
	// Determine client type if not set
	if alert.ClientType == checks.ClientTypeAll {
		for _, client := range checks.CLClients {
			if client == alert.Client {
				alert.ClientType = checks.ClientTypeCL
				break
			}
		}
		if alert.ClientType == checks.ClientTypeAll {
			for _, client := range checks.ELClients {
				if client == alert.Client {
					alert.ClientType = checks.ClientTypeEL
					break
				}
			}
		}
		if alert.ClientType == checks.ClientTypeAll {
			return false, fmt.Errorf("unknown client: %s", alert.Client)
		}
	}

	// Run the checks
	var consensusNode, executionNode string
	if alert.ClientType == checks.ClientTypeCL {
		consensusNode = alert.Client
	} else {
		executionNode = alert.Client
	}

	results, analysis, err := s.checks.RunChecks(ctx, checks.Config{
		Network:       alert.Network,
		ConsensusNode: consensusNode,
		ExecutionNode: executionNode,
		GrafanaToken:  s.config.GrafanaToken,
	})
	if err != nil {
		return false, fmt.Errorf("failed to run checks: %w", err)
	}

	alertSent, err := s.bot.SendResults(
		alert.DiscordChannel,
		alert.Network,
		alert.Client,
		results,
		analysis,
		true,
	)
	if err != nil {
		return alertSent, fmt.Errorf("failed to send discord notification: %w", err)
	}

	return alertSent, nil
}
