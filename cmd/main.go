package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ethpandaops/panda-pulse/pkg/grafana"
	"github.com/ethpandaops/panda-pulse/pkg/service"
	"github.com/spf13/cobra"
)

func main() {
	var cfg service.Config

	rootCmd := &cobra.Command{
		Use:          "panda-pulse",
		Short:        "ethPandaOps dev-net monitoring tool",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := cfg.Validate(); err != nil {
				return fmt.Errorf("invalid configuration: %w", err)
			}

			svc, err := service.NewService(&cfg)
			if err != nil {
				return fmt.Errorf("failed to create service: %w", err)
			}

			if err := svc.Start(); err != nil {
				return fmt.Errorf("failed to start service: %w", err)
			}

			sig := make(chan os.Signal, 1)
			signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
			<-sig
			svc.Stop()

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
	cfg.AccessKeyID = os.Getenv("AWS_ACCESS_KEY_ID")
	cfg.SecretAccessKey = os.Getenv("AWS_SECRET_ACCESS_KEY")
	cfg.S3Bucket = os.Getenv("S3_BUCKET")
	cfg.S3BucketPrefix = os.Getenv("S3_BUCKET_PREFIX")

	if cfg.GrafanaBaseURL == "" {
		cfg.GrafanaBaseURL = grafana.DefaultGrafanaBaseURL
	}

	if cfg.PromDatasourceID == "" {
		cfg.PromDatasourceID = grafana.DefaultPromDatasourceID
	}

	if cfg.S3BucketPrefix == "" {
		cfg.S3BucketPrefix = "ethrand"
	}
}
