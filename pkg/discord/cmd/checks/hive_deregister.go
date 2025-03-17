package checks

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

// handleHiveDeregister handles the '/checks hive-deregister' command.
func (c *ChecksCommand) handleHiveDeregister(
	s *discordgo.Session,
	i *discordgo.InteractionCreate,
	data *discordgo.ApplicationCommandInteractionDataOption,
) error {
	var (
		options = data.Options
		network = options[0].StringValue()
		guildID = i.GuildID // Get the guild ID from the interaction
	)

	c.log.WithFields(logrus.Fields{
		"command": "/checks hive-deregister",
		"network": network,
		"guild":   guildID,
		"user":    i.Member.User.Username,
	}).Info("Received command")

	if err := c.deregisterHiveAlert(context.Background(), network, guildID); err != nil {
		if notRegistered, ok := err.(*hiveNotRegisteredError); ok {
			return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: fmt.Sprintf(msgHiveNotRegistered, notRegistered.Network),
				},
			})
		}

		return fmt.Errorf("failed to deregister Hive alert: %w", err)
	}

	return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf(msgHiveDeregistered, network),
		},
	})
}

// deregisterHiveAlert deregisters a Hive summary alert for a given network.
func (c *ChecksCommand) deregisterHiveAlert(ctx context.Context, network, guildID string) error {
	// First, list all alerts.
	alerts, err := c.bot.GetHiveSummaryRepo().List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list alerts: %w", err)
	}

	// Filter alerts for this guild and network
	var found bool
	var alert *hive.HiveSummaryAlert

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
	jobName := fmt.Sprintf("hive_summary_%s", network)
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
