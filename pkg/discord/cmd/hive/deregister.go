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
		guildID = i.GuildID // Get the guild ID from the interaction
	)

	c.log.WithFields(logrus.Fields{
		"command": "/hive deregister",
		"network": network,
		"guild":   guildID,
		"user":    i.Member.User.Username,
	}).Info("Received command")

	if err := c.deregisterHiveAlert(context.Background(), network, guildID); err != nil {
		if notRegistered, ok := err.(*hiveNotRegisteredError); ok {
			err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: fmt.Sprintf(msgHiveNotRegistered, notRegistered.Network),
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

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf(msgHiveDeregistered, network),
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		c.log.WithError(err).Error("Failed to respond to interaction")
	}
}

// deregisterHiveAlert deregisters a Hive summary alert for a given network.
func (c *HiveCommand) deregisterHiveAlert(ctx context.Context, network, guildID string) error {
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
		if a.Network == network && a.DiscordGuildID == guildID {
			found = true
			alert = a

			break
		}
	}

	if !found {
		return &hiveNotRegisteredError{
			Network: network,
			Guild:   guildID,
		}
	}

	// Remove from S3
	if err := c.bot.GetHiveSummaryRepo().Purge(ctx, network); err != nil {
		return fmt.Errorf("failed to delete alert: %w", err)
	}

	c.log.WithFields(logrus.Fields{
		"network": network,
		"channel": alert.DiscordChannel,
	}).Info("Deregistered Hive summary")

	// Remove from scheduler
	jobName := fmt.Sprintf("hive-summary-%s", network)
	c.bot.GetScheduler().RemoveJob(jobName)

	c.log.WithField("key", jobName).Info("Unscheduled Hive summary alert")

	return nil
}

// hiveNotRegisteredError is returned when a Hive summary is not registered.
type hiveNotRegisteredError struct {
	Network string
	Guild   string
}

// Error implements the error interface.
func (e *hiveNotRegisteredError) Error() string {
	return fmt.Sprintf("Hive summary not registered for network %s", e.Network)
}
