package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ethpandaops/panda-pulse/pkg/roll"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// newRollCommand builds the `panda-pulse roll` subcommand: a gated, sequential
// image rollout across a network's nodes, resolved from cartographoor inventory,
// health-gated on Dora, and triggered via each node's watchtower vhost.
func newRollCommand(log *logrus.Logger) *cobra.Command {
	var (
		network         string
		client          string
		image           string
		inventoryURL    string
		doraURL         string
		watchtowerToken string
		skipHealth      bool
		dryRun          bool
		delay           time.Duration
		postTrigger     time.Duration
		waitTimeout     time.Duration
		healthInterval  time.Duration
	)

	cmd := &cobra.Command{
		Use:          "roll",
		Short:        "Gated rolling image update across a network's nodes",
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			if network == "" {
				return fmt.Errorf("--network is required")
			}

			if watchtowerToken == "" {
				return fmt.Errorf("--watchtower-token (or WATCHTOWER_HTTP_API_TOKEN) is required")
			}

			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			inv, err := roll.FetchInventory(ctx, inventoryURL, network)
			if err != nil {
				return err
			}

			targets := roll.Select(roll.ResolveTargets(inv), client)
			if len(targets) == 0 {
				return fmt.Errorf("no targets matched (network=%s client=%q)", network, client)
			}

			if doraURL == "" {
				doraURL = roll.DoraURLForNetwork(network)
			}

			return roll.NewEngine(roll.NewAPIActuator(watchtowerToken, network), log).Run(ctx, targets, roll.Options{
				Image:               image,
				DelayBetweenNodes:   delay,
				PostTriggerWait:     postTrigger,
				WaitTimeout:         waitTimeout,
				HealthCheckInterval: healthInterval,
				SkipHealth:          skipHealth,
				DryRun:              dryRun,
				DoraURL:             doraURL,
			})
		},
	}

	f := cmd.Flags()
	f.StringVar(&network, "network", "", "network name as published by cartographoor (required)")
	f.StringVar(&client, "client", "", "host pattern: client/group/node with globs, ! to exclude, 'all' (e.g. 'lighthouse', 'lighthouse_ethrex', 'lighthouse-*:!*-1')")
	f.StringVar(&image, "image", "", "scope the roll to this image (empty = all watched containers)")
	f.StringVar(&inventoryURL, "inventory-url", roll.DefaultInventoryBaseURL, "cartographoor inventory base URL")
	f.StringVar(&doraURL, "dora-url", "", "Dora health source URL (default: https://dora.<network>.ethpandaops.io)")
	f.StringVar(&watchtowerToken, "watchtower-token", os.Getenv("WATCHTOWER_HTTP_API_TOKEN"), "watchtower API token (env WATCHTOWER_HTTP_API_TOKEN)")
	f.BoolVar(&skipHealth, "skip-health", false, "skip health gating (force; trigger-and-go even if unhealthy)")
	f.BoolVar(&dryRun, "dry-run", false, "log intent without triggering rolls")
	f.DurationVar(&delay, "delay-roll", time.Minute, "wait between hosts (~N minutes for N hosts); overridable")
	f.DurationVar(&postTrigger, "post-trigger-wait", 30*time.Second, "grace period after triggering before polling recovery")
	f.DurationVar(&waitTimeout, "wait-timeout", 10*time.Minute, "per-node recovery timeout before aborting")
	f.DurationVar(&healthInterval, "health-check-interval", 10*time.Second, "Dora health poll cadence during recovery")

	return cmd
}
