package service

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/ethpandaops/panda-pulse/pkg/discord"
	"github.com/ethpandaops/panda-pulse/pkg/grafana"
	"github.com/ethpandaops/panda-pulse/pkg/hive"
	"github.com/ethpandaops/panda-pulse/pkg/scheduler"
	"github.com/ethpandaops/panda-pulse/pkg/store"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

const (
	defaultHealthPort  = ":9191"
	defaultMetricsPort = ":9091"
	defaultHTTPTimeout = 30 * time.Second
	healthReadTimeout  = 10 * time.Second
	metricsReadTimeout = 10 * time.Second
)

// Service is the main service for the panda-pulse application.
type Service struct {
	config      *Config
	log         *logrus.Logger
	scheduler   *scheduler.Scheduler
	bot         discord.Bot
	monitorRepo *store.MonitorRepo
	checksRepo  *store.ChecksRepo
	healthSrv   *http.Server
	metricsSrv  *http.Server
}

// NewService creates a new Service.
func NewService(ctx context.Context, log *logrus.Logger, cfg *Config) (*Service, error) {
	log.Info("Starting service")

	httpClient := &http.Client{Timeout: defaultHTTPTimeout}

	// Grafana, the source of truth for our data.
	grafanaClient := grafana.NewClient(cfg.AsGrafanaConfig(), httpClient)

	// Hive, used to take screenshots of the test coverage.
	hive := hive.NewHive(cfg.AsHiveConfig())

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
	if verr := monitorRepo.VerifyConnection(ctx); verr != nil {
		return nil, fmt.Errorf("failed to verify S3 connection: %w", verr)
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
		hive,
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
		checksRepo:  checksRepo,
	}, nil
}

func (s *Service) Start(ctx context.Context) error {
	// Start health server.
	s.healthSrv = s.startHealthServer()

	// Start metrics server.
	s.metricsSrv = s.startMetricsServer()

	// Start the discord bot.
	s.log.Info("Starting discord bot")

	if err := s.bot.Start(ctx); err != nil {
		return fmt.Errorf("failed to start discord bot: %w", err)
	}

	// Start the scheduler.
	s.log.Info("Starting scheduler")

	s.scheduler.Start()

	s.log.Info("Service started successfully")

	return nil
}

func (s *Service) Stop(ctx context.Context) error {
	// Stop the scheduler.
	s.log.Info("Stopping scheduler")

	s.scheduler.Stop()

	// Stop the discord bot.
	s.log.Info("Stopping discord bot")

	if err := s.bot.Stop(ctx); err != nil {
		return fmt.Errorf("error stopping discord bot: %w", err)
	}

	// Stop the health server.
	s.log.Info("Stopping health server")

	if err := s.healthSrv.Shutdown(ctx); err != nil {
		return fmt.Errorf("health server shutdown error: %w", err)
	}

	// Stop the metrics server.
	s.log.Info("Stopping metrics server")

	if err := s.metricsSrv.Shutdown(ctx); err != nil {
		return fmt.Errorf("metrics server shutdown error: %w", err)
	}

	s.log.Info("Service stopped successfully")

	return nil
}

func (s *Service) startHealthServer() *http.Server {
	if s.config.HealthCheckAddress == "" {
		s.config.HealthCheckAddress = defaultHealthPort
	}

	s.log.WithFields(logrus.Fields{
		"endpoint": "/healthz",
		"address":  s.config.HealthCheckAddress,
	}).Info("Starting health server")

	mux := http.NewServeMux()
	srv := &http.Server{
		Addr:              s.config.HealthCheckAddress,
		Handler:           mux,
		ReadHeaderTimeout: healthReadTimeout,
	}

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)

		if _, err := w.Write([]byte("ok")); err != nil {
			s.log.Errorf("Failed to write health check response: %v", err)
		}
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
		s.config.MetricsAddress = defaultMetricsPort
	}

	s.log.WithFields(logrus.Fields{
		"endpoint": "/metrics",
		"address":  s.config.MetricsAddress,
	}).Info("Starting metrics server")

	sm := http.NewServeMux()
	sm.Handle("/metrics", promhttp.Handler())

	srv := &http.Server{
		Addr:              s.config.MetricsAddress,
		ReadHeaderTimeout: metricsReadTimeout,
		Handler:           sm,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.log.Errorf("metrics server error: %v", err)
		}
	}()

	return srv
}
