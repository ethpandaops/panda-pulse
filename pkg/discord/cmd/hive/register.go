package hive

import (
	"context"
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/hive"
	"github.com/robfig/cron/v3"
	"github.com/sirupsen/logrus"
)

const (
	msgHiveAlreadyRegistered = "ℹ️ Hive summary is already registered for **%s** in <#%s>"
	msgHiveRegistered        = "✅ Successfully registered Hive summary for **%s** notifications in <#%s>"
	defaultHiveSchedule      = "0 9 * * *" // Daily at 9am UTC
)

// handleRegister handles the register subcommand.
func (c *HiveCommand) handleRegister(s *discordgo.Session, i *discordgo.InteractionCreate, cmd *discordgo.ApplicationCommandInteractionDataOption) {
	var (
		options = cmd.Options
		network = options[0].StringValue()
		channel = options[1].ChannelValue(s)
		guildID = i.GuildID // Get the guild ID from the interaction
	)

	// Get schedule if provided. If its provided, validate its a valid cron schedule.
	schedule := defaultHiveSchedule
	for _, opt := range options {
		if opt.Name == "schedule" {
			schedule = opt.StringValue()

			if _, err := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow).Parse(schedule); err != nil {
				c.respondWithError(s, i, fmt.Sprintf("🚫 Invalid cron schedule: %v", err))
				return
			}

			break
		}
	}

	// Check if it's a text channel.
	if channel.Type != discordgo.ChannelTypeGuildText {
		c.respondWithError(s, i, "🚫 Alerts can only be registered in text channels")
		return
	}

	c.log.WithFields(logrus.Fields{
		"command": "/hive register",
		"network": network,
		"channel": channel.Name,
		"guild":   guildID,
		"user":    i.Member.User.Username,
	}).Info("Received command")

	// Check if Hive is available for this network.
	available, err := c.bot.GetHive().IsAvailable(context.Background(), network)
	if err != nil {
		c.respondWithError(s, i, fmt.Sprintf("Failed to check Hive availability: %v", err))
		return
	}

	if !available {
		c.respondWithError(s, i, fmt.Sprintf("🚫 Hive is not available for network **%s**", network))
		return
	}

	// Check if this network is already registered
	alerts, err := c.bot.GetHiveSummaryRepo().List(context.Background())
	if err != nil {
		c.respondWithError(s, i, fmt.Sprintf("Failed to list alerts: %v", err))
		return
	}

	for _, alert := range alerts {
		if alert.Network == network && alert.DiscordChannel == channel.ID && alert.DiscordGuildID == guildID {
			c.respondWithError(s, i, fmt.Sprintf(msgHiveAlreadyRegistered, network, channel.ID))
			return
		}
	}

	// Create a new alert
	alert := &hive.HiveSummaryAlert{
		Network:        network,
		DiscordChannel: channel.ID,
		DiscordGuildID: guildID,
		Enabled:        true,
		Schedule:       schedule,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	// Persist the alert
	if err := c.bot.GetHiveSummaryRepo().Persist(context.Background(), alert); err != nil {
		c.respondWithError(s, i, fmt.Sprintf("Failed to persist alert: %v", err))
		return
	}

	// Schedule the alert
	jobName := fmt.Sprintf("hive-summary-%s", network)

	c.log.WithFields(logrus.Fields{
		"network": network,
		"channel": channel.Name,
		"key":     jobName,
	}).Info("Registered Hive summary")

	// Schedule the alert to run on our schedule
	if err := c.bot.GetScheduler().AddJob(jobName, alert.Schedule, func(ctx context.Context) error {
		c.log.WithFields(logrus.Fields{
			"network": network,
			"key":     jobName,
		}).Info("Running Hive summary check")

		return c.RunHiveSummary(ctx, alert)
	}); err != nil {
		c.respondWithError(s, i, fmt.Sprintf("Failed to schedule alert: %v", err))
		return
	}

	c.log.WithFields(logrus.Fields{
		"schedule": alert.Schedule,
		"key":      jobName,
	}).Info("Scheduled Hive summary alert")

	// Respond with success
	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf(msgHiveRegistered, network, channel.ID),
		},
	})
	if err != nil {
		c.log.WithError(err).Error("Failed to respond to interaction")
	}
}
