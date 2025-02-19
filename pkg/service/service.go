package service

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/ethpandaops/panda-pulse/pkg/checks"
	"github.com/ethpandaops/panda-pulse/pkg/discord"
	"github.com/ethpandaops/panda-pulse/pkg/grafana"
	"github.com/ethpandaops/panda-pulse/pkg/scheduler"
	"github.com/ethpandaops/panda-pulse/pkg/store"
	"github.com/sirupsen/logrus"
)

type Service struct {
	config      *Config
	log         *logrus.Logger
	scheduler   *scheduler.Scheduler
	bot         *discord.Bot
	monitorRepo *store.MonitorRepo
}

func NewService(ctx context.Context, log *logrus.Logger, cfg *Config) (*Service, error) {
	httpClient := &http.Client{Timeout: 30 * time.Second}

	// Grafana, the source of truth for our data.
	grafanaClient := grafana.NewClient(
		cfg.GrafanaBaseURL,
		cfg.PromDatasourceID,
		cfg.GrafanaToken,
		httpClient,
	)

	// Repository for managing monitor alerts.
	monitorRepo, err := store.NewMonitorRepo(ctx, log, cfg.AsS3Config())
	if err != nil {
		return nil, fmt.Errorf("failed to create S3 store: %w", err)
	}

	// Scheduler for managing the monitor alerts.
	scheduler := scheduler.NewScheduler(log)

	// Checks to execute on for each monitor alert.
	checksRunner := checks.NewDefaultRunner(log)
	checksRunner.RegisterCheck(checks.NewCLSyncCheck(grafanaClient))
	checksRunner.RegisterCheck(checks.NewHeadSlotCheck(grafanaClient))
	checksRunner.RegisterCheck(checks.NewCLFinalizedEpochCheck(grafanaClient))
	checksRunner.RegisterCheck(checks.NewELSyncCheck(grafanaClient))
	checksRunner.RegisterCheck(checks.NewELBlockHeightCheck(grafanaClient))

	// And finally, our bot.
	bot, err := discord.NewBot(
		log,
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
		log:         log,
		bot:         bot,
		scheduler:   scheduler,
		monitorRepo: monitorRepo,
	}, nil
}

func (s *Service) Start(ctx context.Context) error {
	s.log.Info("Starting Discord bot...")

	if err := s.bot.Start(); err != nil {
		return fmt.Errorf("failed to start discord bot: %w", err)
	}

	s.log.Info("Starting scheduler...")
	s.scheduler.Start()

	s.log.Info("Service started successfully")

	return nil
}

func (s *Service) Stop(ctx context.Context) {
	s.log.Info("Stopping service...")
	s.scheduler.Stop()
	if err := s.bot.Stop(); err != nil {
		s.log.Errorf("Error stopping discord bot: %v", err)
	}
}
