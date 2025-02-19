package service

import (
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
	config      *Config
	scheduler   *scheduler.Scheduler
	bot         *discord.Bot
	grafana     grafana.Client
	checks      checks.Runner
	monitorRepo *store.MonitorRepo
}

func NewService(cfg *Config) (*Service, error) {
	httpClient := &http.Client{Timeout: 30 * time.Second}

	// Grafana, the source of truth for our data.
	grafanaClient := grafana.NewClient(
		cfg.GrafanaBaseURL,
		cfg.PromDatasourceID,
		cfg.GrafanaToken,
		httpClient,
	)

	// Repository for managing monitor alerts.
	monitorRepo, err := store.NewMonitorRepo(cfg.AsS3Config())
	if err != nil {
		return nil, fmt.Errorf("failed to create S3 store: %w", err)
	}

	// Scheduler for managing the monitor alerts.
	scheduler := scheduler.NewScheduler()

	// Checks to execute on for each monitor alert.
	checksRunner := checks.NewDefaultRunner()
	checksRunner.RegisterCheck(checks.NewCLSyncCheck(grafanaClient))
	checksRunner.RegisterCheck(checks.NewHeadSlotCheck(grafanaClient))
	checksRunner.RegisterCheck(checks.NewCLFinalizedEpochCheck(grafanaClient))
	checksRunner.RegisterCheck(checks.NewELSyncCheck(grafanaClient))
	checksRunner.RegisterCheck(checks.NewELBlockHeightCheck(grafanaClient))

	// And finally, our bot.
	bot, err := discord.NewBot(
		cfg.AsDiscordConfig(),
		scheduler,
		monitorRepo,
		checksRunner,
		grafanaClient,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create discord bot: %w", err)
	}

	return &Service{
		config:      cfg,
		bot:         bot,
		scheduler:   scheduler,
		monitorRepo: monitorRepo,
	}, nil
}

func (s *Service) Start() error {
	log.Printf("Starting Discord bot...")
	if err := s.bot.Start(); err != nil {
		return fmt.Errorf("failed to start discord bot: %w", err)
	}

	log.Printf("Starting scheduler...")
	s.scheduler.Start()

	log.Printf("Service started successfully")

	return nil
}

func (s *Service) Stop() {
	log.Printf("Stopping service...")
	s.scheduler.Stop()
	if err := s.bot.Stop(); err != nil {
		log.Printf("Error stopping discord bot: %v", err)
	}
}
