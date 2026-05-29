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
// image rollout across a network's nodes, resolved from cartographoor inventory.
func newRollCommand(log *logrus.Logger) *cobra.Command {
	var (
		network          string
		client           string
		image            string
		actuatorKind     string
		inventoryURL     string
		sshKeyPath       string
		watchtowerPort   int
		watchtowerToken  string
		watchtowerScheme string
		watchtowerPrefix string
		beaconScheme     string
		basicAuthUser    string
		basicAuthPass    string
		doraURL          string
		noDora           bool
		skipHealth       bool
		dryRun           bool
		delay            time.Duration
		postTrigger      time.Duration
		waitTimeout      time.Duration
		healthInterval   time.Duration
		maxSyncDistance  uint64
	)

	cmd := &cobra.Command{
		Use:          "roll",
		Short:        "Gated rolling image update across a network's nodes",
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			if network == "" {
				return fmt.Errorf("--network is required")
			}

			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			inv, err := roll.FetchInventory(ctx, inventoryURL, network)
			if err != nil {
				return err
			}

			targets := roll.Select(roll.ResolveTargets(inv, beaconScheme), client)
			if len(targets) == 0 {
				return fmt.Errorf("no targets matched (network=%s client=%q)", network, client)
			}

			actuator, err := buildActuator(actuatorKind, sshKeyPath, watchtowerToken, watchtowerScheme, watchtowerPrefix, watchtowerPort, log)
			if err != nil {
				return err
			}

			doraHealthURL := ""
			if !noDora {
				doraHealthURL = doraURL
				if doraHealthURL == "" {
					doraHealthURL = roll.DoraURLForNetwork(network)
				}
			}

			return roll.NewEngine(actuator, log).Run(ctx, targets, roll.Options{
				Image:               image,
				DelayBetweenNodes:   delay,
				PostTriggerWait:     postTrigger,
				WaitTimeout:         waitTimeout,
				HealthCheckInterval: healthInterval,
				MaxSyncDistance:     maxSyncDistance,
				SkipHealth:          skipHealth,
				DryRun:              dryRun,
				DoraURL:             doraHealthURL,
				BeaconBasicAuthUser: basicAuthUser,
				BeaconBasicAuthPass: basicAuthPass,
			})
		},
	}

	f := cmd.Flags()
	f.StringVar(&network, "network", "", "network name as published by cartographoor (required)")
	f.StringVar(&client, "client", "", "host pattern: client/group/node with globs, ! to exclude, 'all' (e.g. 'lighthouse', 'lighthouse_ethrex', 'lighthouse-*:!*-1')")
	f.StringVar(&image, "image", "", "scope the roll to this image (empty = all watched containers)")
	f.StringVar(&actuatorKind, "actuator", "ssh", "how to trigger the roll: ssh|api")
	f.StringVar(&inventoryURL, "inventory-url", roll.DefaultInventoryBaseURL, "cartographoor inventory base URL")
	f.StringVar(&sshKeyPath, "ssh-key", os.Getenv("ROLL_SSH_KEY"), "SSH private key path (ssh actuator; env ROLL_SSH_KEY)")
	f.IntVar(&watchtowerPort, "watchtower-port", 0, "watchtower API port (api actuator; 0 = default for the scheme)")
	f.StringVar(&watchtowerToken, "watchtower-token", os.Getenv("WATCHTOWER_HTTP_API_TOKEN"), "watchtower API token (api actuator; env WATCHTOWER_HTTP_API_TOKEN)")
	f.StringVar(&watchtowerScheme, "watchtower-scheme", "https", "watchtower API scheme (api actuator)")
	f.StringVar(&watchtowerPrefix, "watchtower-prefix", "watchtower-", "vhost prefix for the watchtower API (api actuator)")
	f.StringVar(&doraURL, "dora-url", "", "Dora health source URL (default: https://dora.<network>.ethpandaops.io)")
	f.BoolVar(&noDora, "no-dora", false, "use per-node beacon health instead of Dora")
	f.StringVar(&beaconScheme, "beacon-scheme", "https", "scheme for beacon health endpoints (only used with --no-dora)")
	f.StringVar(&basicAuthUser, "basic-auth-user", os.Getenv("ROLL_BASIC_AUTH_USER"), "basic auth user for beacon health endpoints (env ROLL_BASIC_AUTH_USER)")
	f.StringVar(&basicAuthPass, "basic-auth-pass", os.Getenv("ROLL_BASIC_AUTH_PASS"), "basic auth password for beacon health endpoints (env ROLL_BASIC_AUTH_PASS)")
	f.BoolVar(&skipHealth, "skip-health", false, "skip beacon health gating (force; trigger-and-go even if unhealthy)")
	f.BoolVar(&dryRun, "dry-run", false, "log intent without triggering rolls")
	f.DurationVar(&delay, "delay-roll", time.Minute, "wait between hosts (~N minutes for N hosts); overridable")
	f.DurationVar(&postTrigger, "post-trigger-wait", 30*time.Second, "grace period after triggering before polling recovery")
	f.DurationVar(&waitTimeout, "wait-timeout", 10*time.Minute, "per-node recovery timeout before aborting")
	f.DurationVar(&healthInterval, "health-check-interval", 10*time.Second, "beacon health poll cadence during recovery")
	f.Uint64Var(&maxSyncDistance, "max-sync-distance", 4, "max sync distance (slots) still considered healthy")

	return cmd
}

func buildActuator(kind, sshKeyPath, token, scheme, prefix string, port int, log *logrus.Logger) (roll.Actuator, error) {
	switch kind {
	case "ssh":
		return roll.NewSSHActuator(roll.SSHConfig{
			PrivateKeyPath: sshKeyPath,
			ContainerName:  roll.DefaultWatchtowerContainer,
			Port:           roll.DefaultWatchtowerPort,
			Log:            log,
		})
	case "api":
		if token == "" {
			return nil, fmt.Errorf("--watchtower-token (or WATCHTOWER_HTTP_API_TOKEN) is required for the api actuator")
		}

		return roll.NewAPIActuator(token, scheme, port, prefix), nil
	default:
		return nil, fmt.Errorf("unknown actuator %q (want ssh or api)", kind)
	}
}
