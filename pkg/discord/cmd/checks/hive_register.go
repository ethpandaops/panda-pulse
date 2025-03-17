package checks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/hive"
	"github.com/sirupsen/logrus"
)

const (
	msgHiveAlreadyRegistered = "ℹ️ Hive summary is already registered for **%s** in <#%s>"
	msgHiveRegistered        = "✅ Successfully registered Hive summary for **%s** notifications in <#%s>"
	defaultHiveSchedule      = "*/1 * * * *" // Daily at 8am UTC
)

// handleHiveRegister handles the '/checks hive-register' command.
func (c *ChecksCommand) handleHiveRegister(
	s *discordgo.Session,
	i *discordgo.InteractionCreate,
	data *discordgo.ApplicationCommandInteractionDataOption,
) error {
	var (
		options = data.Options
		network = options[0].StringValue()
		channel = options[1].ChannelValue(s)
		guildID = i.GuildID // Get the guild ID from the interaction
	)

	// Check if it's a text channel.
	if channel.Type != discordgo.ChannelTypeGuildText {
		return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "🚫 Hive summaries can only be registered in text channels",
			},
		})
	}

	// Check if channel is in the 'bots' category.
	if parentChannel, err := s.Channel(channel.ParentID); err == nil {
		if !strings.EqualFold(parentChannel.Name, "bots") && !strings.EqualFold(parentChannel.Name, "monitoring") {
			return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "🚫 Hive summaries can only be registered in channels under the `bots` or `monitoring` category",
				},
			})
		}
	}

	c.log.WithFields(logrus.Fields{
		"command": "/checks hive-register",
		"network": network,
		"channel": channel.Name,
		"guild":   guildID,
		"user":    i.Member.User.Username,
	}).Info("Received command")

	// Check if Hive is available for this network
	available, err := c.bot.GetHive().IsAvailable(context.Background(), network)
	if err != nil {
		return fmt.Errorf("failed to check Hive availability: %w", err)
	}

	if !available {
		return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("🚫 Hive is not available for network **%s**", network),
			},
		})
	}

	if err := c.registerHiveAlert(context.Background(), network, channel.ID, guildID); err != nil {
		if alreadyRegistered, ok := err.(*hiveAlreadyRegisteredError); ok {
			return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: fmt.Sprintf(msgHiveAlreadyRegistered, alreadyRegistered.Network, channel.ID),
				},
			})
		}

		return fmt.Errorf("failed to register Hive alert: %w", err)
	}

	return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf(msgHiveRegistered, network, channel.ID),
		},
	})
}

// registerHiveAlert registers a Hive summary alert for a given network.
func (c *ChecksCommand) registerHiveAlert(ctx context.Context, network, channelID, guildID string) error {
	// Check if this network is already registered.
	alerts, err := c.bot.GetHiveSummaryRepo().List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list alerts: %w", err)
	}

	for _, alert := range alerts {
		if alert.Network == network && alert.DiscordChannel == channelID && alert.DiscordGuildID == guildID {
			return &hiveAlreadyRegisteredError{
				Network: network,
				Channel: channelID,
				Guild:   guildID,
			}
		}
	}

	// Create a new alert.
	alert := &hive.HiveSummaryAlert{
		Network:        network,
		DiscordChannel: channelID,
		DiscordGuildID: guildID,
		Enabled:        true,
		Schedule:       defaultHiveSchedule,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	// Persist the alert.
	if err := c.bot.GetHiveSummaryRepo().Persist(ctx, alert); err != nil {
		return fmt.Errorf("failed to persist alert: %w", err)
	}

	// Schedule the alert.
	jobName := fmt.Sprintf("hive_summary_%s", network)

	c.log.WithFields(logrus.Fields{
		"network": network,
		"channel": channelID,
		"key":     jobName,
	}).Info("Registered Hive summary")

	// Schedule the alert to run on our schedule.
	if err := c.bot.GetScheduler().AddJob(jobName, alert.Schedule, func(ctx context.Context) error {
		c.log.WithFields(logrus.Fields{
			"network": network,
			"key":     jobName,
		}).Info("Running Hive summary check")

		return c.RunHiveSummary(ctx, alert)
	}); err != nil {
		return fmt.Errorf("failed to schedule alert: %w", err)
	}

	c.log.WithFields(logrus.Fields{
		"schedule": alert.Schedule,
		"key":      jobName,
	}).Info("Scheduled Hive summary alert")

	return nil
}

// hiveAlreadyRegisteredError is returned when a Hive summary is already registered.
type hiveAlreadyRegisteredError struct {
	Network string
	Channel string
	Guild   string
}

// Error implements the error interface.
func (e *hiveAlreadyRegisteredError) Error() string {
	return fmt.Sprintf("Hive summary already registered for network %s in channel %s", e.Network, e.Channel)
}
