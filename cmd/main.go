package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ethpandaops/panda-pulse/pkg/checks"
	"github.com/ethpandaops/panda-pulse/pkg/discord"
	"github.com/ethpandaops/panda-pulse/pkg/grafana"
	"github.com/spf13/cobra"
)

const (
	defaultGrafanaBaseURL   = "https://grafana.observability.ethpandaops.io"
	defaultPromDatasourceID = "UhcO3vy7z"
)

// Config contains the configuration for the panda-pulse tool.
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
	BotMode          bool // Whether to run in bot mode instead of one-shot mode
}

func main() {
	// Remove timestamp from log output, makes it harder to grok.
	log.SetFlags(0)

	var cfg Config

	rootCmd := &cobra.Command{
		Use:          "panda-pulse",
		Short:        "EthPandaOps dev-net monitoring tool",
		SilenceUsage: true,
	}

	// One-shot command (original cron behavior)
	checkCmd := &cobra.Command{
		Use:   "check",
		Short: "Run checks once and exit",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOneShot(cmd, cfg)
		},
	}

	// Bot mode command
	botCmd := &cobra.Command{
		Use:   "bot",
		Short: "Run as a Discord bot",
		RunE: func(cmd *cobra.Command, args []string) error {
			if cfg.GrafanaToken == "" {
				return fmt.Errorf("GRAFANA_SERVICE_TOKEN environment variable is required")
			}
			if cfg.DiscordToken == "" {
				return fmt.Errorf("DISCORD_BOT_TOKEN environment variable is required")
			}

			return runBot(cmd, cfg)
		},
	}

	// Add flags to check command
	checkCmd.Flags().StringVar(&cfg.Network, "network", "", "network to monitor (e.g., pectra-devnet-5)")
	checkCmd.Flags().StringVar(&cfg.DiscordChannel, "discord-channel", "", "discord channel to notify")
	checkCmd.Flags().StringVar(&cfg.ConsensusNode, "ethereum-cl", checks.ClientTypeAll.String(), "consensus client to monitor")
	checkCmd.Flags().StringVar(&cfg.ExecutionNode, "ethereum-el", checks.ClientTypeAll.String(), "execution client to monitor")
	checkCmd.Flags().StringVar(&cfg.GrafanaBaseURL, "grafana-base-url", defaultGrafanaBaseURL, "grafana base URL")
	checkCmd.Flags().StringVar(&cfg.PromDatasourceID, "prometheus-datasource-id", defaultPromDatasourceID, "prometheus datasource ID")
	checkCmd.Flags().BoolVar(&cfg.AlertUnexplained, "alert-unexplained", true, "whether to alert on unexplained issues")

	if err := checkCmd.MarkFlagRequired("network"); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if err := checkCmd.MarkFlagRequired("discord-channel"); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Add flags to bot command
	botCmd.Flags().StringVar(&cfg.GrafanaBaseURL, "grafana-base-url", defaultGrafanaBaseURL, "grafana base URL")
	botCmd.Flags().StringVar(&cfg.PromDatasourceID, "prometheus-datasource-id", defaultPromDatasourceID, "prometheus datasource ID")
	botCmd.Flags().BoolVar(&cfg.AlertUnexplained, "alert-unexplained", true, "whether to alert on unexplained issues")

	// Add commands to root
	rootCmd.AddCommand(checkCmd)
	rootCmd.AddCommand(botCmd)

	cfg.GrafanaToken = os.Getenv("GRAFANA_SERVICE_TOKEN")
	cfg.DiscordToken = os.Getenv("DISCORD_BOT_TOKEN")
	cfg.OpenRouterKey = os.Getenv("OPENROUTER_API_KEY")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// runOneShot is the original cron-job behavior
func runOneShot(cmd *cobra.Command, cfg Config) error {
	if cfg.GrafanaToken == "" {
		return fmt.Errorf("GRAFANA_SERVICE_TOKEN environment variable is required")
	}
	if cfg.DiscordToken == "" {
		return fmt.Errorf("DISCORD_BOT_TOKEN environment variable is required")
	}

	// We enforce that one of --ethereum-cl or --ethereum-el is specified.
	clSpecified := cmd.Flags().Changed("ethereum-cl")
	elSpecified := cmd.Flags().Changed("ethereum-el")

	if !clSpecified && !elSpecified {
		return fmt.Errorf("must specify either --ethereum-cl or --ethereum-el")
	}

	if clSpecified && elSpecified {
		return fmt.Errorf("cannot specify both --ethereum-cl and --ethereum-el flags")
	}

	if clSpecified {
		if err := validateClient(cfg.ConsensusNode, true); err != nil {
			return err
		}
	}

	if elSpecified {
		if err := validateClient(cfg.ExecutionNode, false); err != nil {
			return err
		}
	}

	return runChecks(cmd, cfg)
}

// runBot runs the Discord bot mode
func runBot(cmd *cobra.Command, cfg Config) error {
	// Create shared HTTP client.
	httpClient := &http.Client{Timeout: 30 * time.Second}

	// Initialize Grafana client.
	grafanaClient := grafana.NewClient(cfg.GrafanaBaseURL, cfg.PromDatasourceID, cfg.GrafanaToken, httpClient)

	// Initialize Discord notifier.
	discordClient, err := discord.NewClient(cfg.DiscordToken, cfg.OpenRouterKey, cfg.GrafanaToken)
	if err != nil {
		return fmt.Errorf("failed to create Discord client: %w", err)
	}

	// Initialize check runner.
	runner := checks.NewDefaultRunner()

	// Register checks.
	runner.RegisterCheck(checks.NewCLSyncCheck(grafanaClient))
	runner.RegisterCheck(checks.NewHeadSlotCheck(grafanaClient))
	runner.RegisterCheck(checks.NewCLFinalizedEpochCheck(grafanaClient))
	runner.RegisterCheck(checks.NewELSyncCheck(grafanaClient))
	runner.RegisterCheck(checks.NewELBlockHeightCheck(grafanaClient))

	// Start bot
	if err := discordClient.StartBot(runner); err != nil {
		return fmt.Errorf("failed to start bot: %w", err)
	}

	return nil
}

func runChecks(cmd *cobra.Command, cfg Config) error {
	// Create shared HTTP client.
	httpClient := &http.Client{Timeout: 30 * time.Second}

	// Initialize Grafana client.
	grafanaClient := grafana.NewClient(cfg.GrafanaBaseURL, cfg.PromDatasourceID, cfg.GrafanaToken, httpClient)

	// Initialize Discord client.
	discordClient, err := discord.NewClient(cfg.DiscordToken, cfg.OpenRouterKey, cfg.GrafanaToken)
	if err != nil {
		return fmt.Errorf("failed to create Discord client: %w", err)
	}

	// Initialize check runner.
	runner := checks.NewDefaultRunner()

	// Register checks.
	runner.RegisterCheck(checks.NewCLSyncCheck(grafanaClient))
	runner.RegisterCheck(checks.NewHeadSlotCheck(grafanaClient))
	runner.RegisterCheck(checks.NewCLFinalizedEpochCheck(grafanaClient))
	runner.RegisterCheck(checks.NewELSyncCheck(grafanaClient))
	runner.RegisterCheck(checks.NewELBlockHeightCheck(grafanaClient))

	// Determine if we're running checks for a specific client.
	var targetClient string
	if cmd.Flags().Changed("ethereum-cl") {
		targetClient = cfg.ConsensusNode
	} else if cmd.Flags().Changed("ethereum-el") {
		targetClient = cfg.ExecutionNode
	}

	// Execute the checks.
	results, analysis, err := runner.RunChecks(context.Background(), checks.Config{
		Network:       cfg.Network,
		ConsensusNode: cfg.ConsensusNode,
		ExecutionNode: cfg.ExecutionNode,
		GrafanaToken:  cfg.GrafanaToken,
	})
	if err != nil {
		return fmt.Errorf("failed to run checks: %w", err)
	}

	// Send results to Discord.
	if err := discordClient.SendResults(cfg.DiscordChannel, cfg.Network, targetClient, results, analysis, cfg.AlertUnexplained); err != nil {
		return fmt.Errorf("failed to send discord notification: %w", err)
	}

	return nil
}

// validateClient validates any client flag passed.
func validateClient(client string, isCL bool) error {
	// Allow wildcard.
	if client == checks.ClientTypeAll.String() {
		return nil
	}

	if isCL {
		if !checks.IsCLClient(client) {
			return fmt.Errorf("invalid consensus client '%s'. Must be one of: %s", client, strings.Join(checks.CLClients, ", "))
		}
	} else {
		if !checks.IsELClient(client) {
			return fmt.Errorf("invalid execution client '%s'. Must be one of: %s", client, strings.Join(checks.ELClients, ", "))
		}
	}

	return nil
}
