package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ethpandaops/panda-pulse/pkg/grafana"
	"github.com/ethpandaops/panda-pulse/pkg/service"
	"github.com/ethpandaops/panda-pulse/pkg/store"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const shutdownTimeout = 30 * time.Second

func main() {
	var cfg service.Config

	// Initialize logger.
	log := logrus.New()
	log.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	// Create root context.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rootCmd := &cobra.Command{
		Use:          "panda-pulse",
		Short:        "ethPandaOps dev-net monitoring tool",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := cfg.Validate(); err != nil {
				return fmt.Errorf("invalid configuration: %w", err)
			}

			svc, err := service.NewService(ctx, log, &cfg)
			if err != nil {
				return fmt.Errorf("failed to create service: %w", err)
			}

			if err := svc.Start(ctx); err != nil {
				return fmt.Errorf("failed to start service: %w", err)
			}

			sig := make(chan os.Signal, 1)
			signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

			select {
			case <-sig:
				log.Info("Received shutdown signal...")
			case <-ctx.Done():
				log.Info("Context cancelled...")
			}

			// Create a new context with timeout for shutdown.
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
			defer shutdownCancel()

			log.Info("Shutting down...")

			if err := svc.Stop(shutdownCtx); err != nil {
				log.Errorf("Failed to stop service: %v", err)
			}

			return nil
		},
	}

	setConfig(&cfg)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func setConfig(cfg *service.Config) {
	cfg.GrafanaToken = os.Getenv("GRAFANA_SERVICE_TOKEN")
	cfg.GrafanaBaseURL = os.Getenv("GRAFANA_BASE_URL")
	cfg.PromDatasourceID = os.Getenv("PROMETHEUS_DATASOURCE_ID")
	cfg.DiscordToken = os.Getenv("DISCORD_BOT_TOKEN")
	cfg.DiscordGuildID = os.Getenv("DISCORD_GUILD_ID")
	cfg.AccessKeyID = os.Getenv("AWS_ACCESS_KEY_ID")
	cfg.SecretAccessKey = os.Getenv("AWS_SECRET_ACCESS_KEY")
	cfg.GithubToken = os.Getenv("GITHUB_TOKEN")
	cfg.S3Bucket = os.Getenv("S3_BUCKET")
	cfg.S3BucketPrefix = os.Getenv("S3_BUCKET_PREFIX")
	cfg.S3Region = os.Getenv("AWS_REGION")
	cfg.S3EndpointURL = os.Getenv("AWS_ENDPOINT_URL")
	cfg.HealthCheckAddress = os.Getenv("HEALTH_CHECK_ADDRESS")
	cfg.MetricsAddress = os.Getenv("METRICS_ADDRESS")

	if cfg.GrafanaBaseURL == "" {
		cfg.GrafanaBaseURL = grafana.DefaultGrafanaBaseURL
	}

	if cfg.PromDatasourceID == "" {
		cfg.PromDatasourceID = grafana.DefaultPromDatasourceID
	}

	if cfg.S3Region == "" {
		cfg.S3Region = store.DefaultRegion
	}

	if cfg.S3BucketPrefix == "" {
		cfg.S3BucketPrefix = store.DefaultBucketPrefix
	}
}
