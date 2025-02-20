package service

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/ethpandaops/panda-pulse/pkg/discord"
	"github.com/ethpandaops/panda-pulse/pkg/grafana"
	"github.com/ethpandaops/panda-pulse/pkg/scheduler"
	"github.com/ethpandaops/panda-pulse/pkg/store"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

type Service struct {
	config      *Config
	log         *logrus.Logger
	scheduler   *scheduler.Scheduler
	bot         *discord.Bot
	monitorRepo *store.MonitorRepo
	healthSrv   *http.Server
	metricsSrv  *http.Server
}

func NewService(ctx context.Context, log *logrus.Logger, cfg *Config) (*Service, error) {
	log.Info("Starting service")

	httpClient := &http.Client{Timeout: 30 * time.Second}

	// Grafana, the source of truth for our data.
	grafanaClient := grafana.NewClient(cfg.AsGrafanaConfig(), httpClient)

	// Repository for managing monitor alerts.
	monitorRepo, err := store.NewMonitorRepo(ctx, log, cfg.AsS3Config())
	if err != nil {
		return nil, fmt.Errorf("failed to create S3 store: %w", err)
	}

	// Repository for managing checks.
	checksRepo, err := store.NewChecksRepo(ctx, log, cfg.AsS3Config())
	if err != nil {
		return nil, fmt.Errorf("failed to create S3 store: %w", err)
	}

	// Check S3 connection health, no point in continuing if we can't access the store.
	if err := monitorRepo.VerifyConnection(ctx); err != nil {
		return nil, fmt.Errorf("failed to verify S3 connection: %w", err)
	}

	// Scheduler for managing the monitor alerts.
	scheduler := scheduler.NewScheduler(log)

	// And finally, our bot.
	bot, err := discord.NewBot(
		log,
		cfg.AsDiscordConfig(),
		scheduler,
		monitorRepo,
		checksRepo,
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
	// Start health server.
	s.healthSrv = s.startHealthServer()

	// Start metrics server.
	s.metricsSrv = s.startMetricsServer()

	// Start the discord bot.
	s.log.Info("Starting discord bot")
	if err := s.bot.Start(); err != nil {
		return fmt.Errorf("failed to start discord bot: %w", err)
	}

	// Start the scheduler.
	s.log.Info("Starting scheduler")
	s.scheduler.Start()

	s.log.Info("Service started successfully")

	return nil
}

func (s *Service) Stop(ctx context.Context) {
	// Stop the scheduler.
	s.log.Info("Stopping scheduler")
	s.scheduler.Stop()

	// Stop the discord bot.
	s.log.Info("Stopping discord bot")
	if err := s.bot.Stop(); err != nil {
		s.log.Errorf("Error stopping discord bot: %v", err)
	}

	// Stop the health server.
	s.log.Info("Stopping health server")
	if err := s.healthSrv.Shutdown(ctx); err != nil {
		s.log.Errorf("Health server shutdown error: %v", err)
	}

	// Stop the metrics server.
	s.log.Info("Stopping metrics server")
	if err := s.metricsSrv.Shutdown(ctx); err != nil {
		s.log.Errorf("Metrics server shutdown error: %v", err)
	}

	s.log.Info("Service stopped successfully")
}

func (s *Service) startHealthServer() *http.Server {
	if s.config.HealthCheckAddress == "" {
		s.config.HealthCheckAddress = ":9191"
	}

	s.log.WithFields(logrus.Fields{
		"endpoint": "/healthz",
		"address":  s.config.HealthCheckAddress,
	}).Info("Starting health server")

	mux := http.NewServeMux()
	srv := &http.Server{
		Addr:    s.config.HealthCheckAddress,
		Handler: mux,
	}

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.log.Errorf("health server error: %v", err)
		}
	}()

	return srv
}

func (s *Service) startMetricsServer() *http.Server {
	if s.config.MetricsAddress == "" {
		s.config.MetricsAddress = ":9091"
	}

	s.log.WithFields(logrus.Fields{
		"endpoint": "/metrics",
		"address":  s.config.MetricsAddress,
	}).Info("Starting metrics server")

	sm := http.NewServeMux()
	sm.Handle("/metrics", promhttp.Handler())

	srv := &http.Server{
		Addr:              s.config.MetricsAddress,
		ReadHeaderTimeout: 15 * time.Second,
		Handler:           sm,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.log.Errorf("metrics server error: %v", err)
		}
	}()

	return srv
}
