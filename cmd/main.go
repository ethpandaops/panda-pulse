package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ethpandaops/panda-pulse/pkg/service"
	"github.com/ethpandaops/panda-pulse/pkg/store"
	"github.com/spf13/cobra"
)

const (
	defaultGrafanaBaseURL   = "https://grafana.observability.ethpandaops.io"
	defaultPromDatasourceID = "UhcO3vy7z"
)

func main() {
	var cfg service.Config

	rootCmd := &cobra.Command{
		Use:          "panda-pulse",
		Short:        "EthPandaOps dev-net monitoring tool",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if cfg.GrafanaToken == "" {
				return fmt.Errorf("GRAFANA_SERVICE_TOKEN environment variable is required")
			}
			if cfg.DiscordToken == "" {
				return fmt.Errorf("DISCORD_BOT_TOKEN environment variable is required")
			}

			// Initialize AWS and service
			store, err := store.NewS3Store(&store.S3Config{
				AccessKeyID:     os.Getenv("AWS_ACCESS_KEY_ID"),
				SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
				Bucket:          os.Getenv("S3_BUCKET"),
				Prefix:          "bot",
			})
			if err != nil {
				return fmt.Errorf("failed to create S3 store: %w", err)
			}

			svc, err := service.NewService(&service.Config{
				Store:            store,
				GrafanaBaseURL:   defaultGrafanaBaseURL,
				GrafanaToken:     cfg.GrafanaToken,
				DiscordToken:     cfg.DiscordToken,
				PromDatasourceID: defaultPromDatasourceID,
				OpenRouterKey:    cfg.OpenRouterKey,
			})
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

	// Environment variables
	cfg.GrafanaToken = os.Getenv("GRAFANA_SERVICE_TOKEN")
	cfg.DiscordToken = os.Getenv("DISCORD_BOT_TOKEN")
	cfg.OpenRouterKey = os.Getenv("OPENROUTER_API_KEY")
	cfg.AlertUnexplained = true

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
