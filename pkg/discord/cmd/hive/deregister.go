package hive

import (
	"context"
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/hive"
	"github.com/sirupsen/logrus"
)

const (
	msgHiveNotRegistered = "ℹ️ Hive summary is not registered for **%s**"
	msgHiveDeregistered  = "✅ Successfully deregistered Hive summary for **%s**"
)

// handleDeregister handles the deregister subcommand.
func (c *HiveCommand) handleDeregister(s *discordgo.Session, i *discordgo.InteractionCreate, cmd *discordgo.ApplicationCommandInteractionDataOption) {
	var (
		options = cmd.Options
		network = options[0].StringValue()
		suite   = ""
		guildID = i.GuildID // Get the guild ID from the interaction
	)

	// Extract the suite parameter if provided
	for _, opt := range cmd.Options {
		if opt.Name == optionNameSuite {
			suite = opt.StringValue()

			break
		}
	}

	if err := c.deregisterHiveAlert(context.Background(), network, suite, guildID); err != nil {
		if notRegistered, ok := err.(*hiveNotRegisteredError); ok {
			msg := fmt.Sprintf(msgHiveNotRegistered, notRegistered.Network)
			if notRegistered.Suite != "" {
				msg = fmt.Sprintf("ℹ️ Hive summary for **%s** (suite: %s) is not registered", notRegistered.Network, notRegistered.Suite)
			}

			err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: msg,
				},
			})
			if err != nil {
				c.log.WithError(err).Error("Failed to respond to interaction")
			}

			return
		}

		c.respondWithError(s, i, fmt.Sprintf("Failed to deregister Hive alert: %v", err))

		return
	}

	successMsg := fmt.Sprintf(msgHiveDeregistered, network)
	if suite != "" {
		successMsg = fmt.Sprintf("✅ Successfully deregistered Hive summary for **%s** (suite: %s)", network, suite)
	}

	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: successMsg,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	}); err != nil {
		c.log.WithError(err).Error("Failed to respond to interaction")
	}
}

// deregisterHiveAlert deregisters a Hive summary alert for a given network.
func (c *HiveCommand) deregisterHiveAlert(ctx context.Context, network, suite, guildID string) error {
	// First, list all alerts.
	alerts, err := c.bot.GetHiveSummaryRepo().List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list alerts: %w", err)
	}

	// Filter alerts for this guild and network.
	var (
		found bool
		alert *hive.HiveSummaryAlert
	)

	for _, a := range alerts {
		if a.Network == network && a.Suite == suite && a.DiscordGuildID == guildID {
			found = true
			alert = a

			break
		}
	}

	if !found {
		return &hiveNotRegisteredError{
			Network: network,
			Suite:   suite,
			Guild:   guildID,
		}
	}

	// Remove from S3 with suite-specific path handling
	if suite != "" {
		if err := c.bot.GetHiveSummaryRepo().Purge(ctx, network, suite); err != nil {
			return fmt.Errorf("failed to delete alert: %w", err)
		}
	} else {
		if err := c.bot.GetHiveSummaryRepo().Purge(ctx, network); err != nil {
			return fmt.Errorf("failed to delete alert: %w", err)
		}
	}

	// Remove from scheduler
	jobName := fmt.Sprintf("hive-summary-%s", network)
	if suite != "" {
		jobName = fmt.Sprintf("hive-summary-%s-%s", network, suite)
	}

	c.bot.GetScheduler().RemoveJob(jobName)

	c.log.WithFields(logrus.Fields{
		"network": network,
		"suite":   suite,
		"channel": alert.DiscordChannel,
	}).Info("Deregistered Hive summary")

	return nil
}

// hiveNotRegisteredError is returned when a Hive summary is not registered.
type hiveNotRegisteredError struct {
	Network string
	Suite   string
	Guild   string
}

// Error implements the error interface.
func (e *hiveNotRegisteredError) Error() string {
	if e.Suite != "" {
		return fmt.Sprintf("Hive summary not registered for network %s (suite: %s)", e.Network, e.Suite)
	}

	return fmt.Sprintf("Hive summary not registered for network %s", e.Network)
}
