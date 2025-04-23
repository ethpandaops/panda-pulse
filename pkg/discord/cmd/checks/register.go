package checks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/clients"
	"github.com/ethpandaops/panda-pulse/pkg/store"
	"github.com/robfig/cron/v3"
	"github.com/sirupsen/logrus"
)

const (
	msgAlreadyRegistered = "ℹ️ Client **%s** is already registered for **%s** in <#%s>"
	msgRegisteredClient  = "✅ Successfully registered **%s** for **%s** notifications in <#%s>"
	msgRegisteredAll     = "✅ Successfully registered **all clients** for **%s** notifications in <#%s>"
)

// handleRegister handles the '/checks register' command.
func (c *ChecksCommand) handleRegister(
	s *discordgo.Session,
	i *discordgo.InteractionCreate,
	data *discordgo.ApplicationCommandInteractionDataOption,
) error {
	var (
		options  = data.Options
		network  = options[0].StringValue()
		channel  = options[1].ChannelValue(s)
		client   *string
		guildID  = i.GuildID // Get the guild ID from the interaction
		schedule = DefaultCheckSchedule
	)

	// Check if it's a text channel.
	if channel.Type != discordgo.ChannelTypeGuildText {
		return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "🚫 Alerts can only be registered in text channels",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
	}

	// Check if channel is in the 'bots' category.
	if parentChannel, err := s.Channel(channel.ParentID); err == nil {
		if !strings.EqualFold(parentChannel.Name, "bots") && !strings.EqualFold(parentChannel.Name, "monitoring") {
			return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "🚫 Alerts can only be registered in channels under the `bots` or `monitoring` category",
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
		}
	}

	for _, opt := range options {
		if opt.Name == "client" {
			c := opt.StringValue()
			client = &c

			break
		}
	}

	// Get schedule if provided, and ensure its valid.
	for _, opt := range options {
		if opt.Name == "schedule" {
			schedule = opt.StringValue()

			// Validate the cron schedule
			if _, err := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow).Parse(schedule); err != nil {
				return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{
						Content: fmt.Sprintf("🚫 Invalid cron schedule: %v", err),
						Flags:   discordgo.MessageFlagsEphemeral,
					},
				})
			}

			break
		}
	}

	if err := c.registerAlert(context.Background(), network, channel.ID, guildID, client, schedule); err != nil {
		if alreadyRegistered, ok := err.(*store.AlertAlreadyRegisteredError); ok {
			return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: fmt.Sprintf(msgAlreadyRegistered, alreadyRegistered.Client, network, channel.ID),
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
		}

		return fmt.Errorf("failed to register alert: %w", err)
	}

	var msg string

	if client != nil {
		msg = fmt.Sprintf(msgRegisteredClient, *client, network, channel.ID)
	} else {
		msg = fmt.Sprintf(msgRegisteredAll, network, channel.ID)
	}

	return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: msg,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

func (c *ChecksCommand) registerAlert(ctx context.Context, network, channelID, guildID string, specificClient *string, schedule string) error {
	if specificClient == nil {
		return c.registerAllClients(ctx, network, channelID, guildID, schedule)
	}

	// Check if this specific client is already registered.
	alerts, err := c.bot.GetMonitorRepo().List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list alerts: %w", err)
	}

	for _, alert := range alerts {
		if alert.Network == network && alert.Client == *specificClient && alert.DiscordChannel == channelID && alert.DiscordGuildID == guildID {
			return &store.AlertAlreadyRegisteredError{
				Network: network,
				Channel: channelID,
				Guild:   guildID,
				Client:  *specificClient,
			}
		}
	}

	clientType := getClientType(*specificClient)
	if clientType == clients.ClientTypeAll {
		return fmt.Errorf("unknown client: %s", *specificClient)
	}

	alert := newMonitorAlert(network, *specificClient, clientType, channelID, guildID)
	alert.Schedule = schedule

	if err := c.scheduleAlert(ctx, alert); err != nil {
		return fmt.Errorf("failed to schedule alert: %w", err)
	}

	return nil
}

// registerAllClients registers a monitor alert for all clients for a given network.
func (c *ChecksCommand) registerAllClients(ctx context.Context, network, channelID, guildID string, schedule string) error {
	// Register CL clients.
	for _, client := range clients.CLClients {
		alert := newMonitorAlert(network, client, clients.ClientTypeCL, channelID, guildID)
		alert.Schedule = schedule

		if err := c.scheduleAlert(ctx, alert); err != nil {
			return fmt.Errorf("failed to schedule CL alert: %w", err)
		}
	}

	// Register EL clients.
	for _, client := range clients.ELClients {
		alert := newMonitorAlert(network, client, clients.ClientTypeEL, channelID, guildID)
		alert.Schedule = schedule

		if err := c.scheduleAlert(ctx, alert); err != nil {
			return fmt.Errorf("failed to schedule EL alert: %w", err)
		}
	}

	return nil
}

// scheduleAlert schedules a monitor alert to run every minute.
func (c *ChecksCommand) scheduleAlert(ctx context.Context, alert *store.MonitorAlert) error {
	// Firstly, persist the alert to our store.
	if err := c.bot.GetMonitorRepo().Persist(ctx, alert); err != nil {
		return err
	}

	jobName := c.bot.GetMonitorRepo().Key(alert)

	c.log.WithFields(logrus.Fields{
		"channel": alert.DiscordChannel,
		"client":  alert.Client,
	}).Info("Registered alert")

	// And secondly, schedule the alert to run on our schedule.
	if addErr := c.bot.GetScheduler().AddJob(jobName, alert.Schedule, func(ctx context.Context) error {
		c.log.WithFields(logrus.Fields{
			"client": alert.Client,
			"key":    jobName,
		}).Info("Queueing alert")

		c.Queue().Enqueue(alert)

		return nil
	}); addErr != nil {
		return fmt.Errorf("failed to schedule alert: %w", addErr)
	}

	c.log.WithFields(logrus.Fields{
		"schedule": alert.Schedule,
		"key":      jobName,
	}).Info("Scheduled alert")

	return nil
}

// newMonitorAlert creates a new monitor alert with the given parameters.
func newMonitorAlert(network, client string, clientType clients.ClientType, channelID, guildID string) *store.MonitorAlert {
	now := time.Now()

	return &store.MonitorAlert{
		Network:        network,
		Client:         client,
		ClientType:     clientType,
		DiscordChannel: channelID,
		DiscordGuildID: guildID,
		Schedule:       DefaultCheckSchedule,
		CreatedAt:      now,
		UpdatedAt:      now,
		Enabled:        true,
	}
}

// getClientType determines the client type from a client name.
func getClientType(clientName string) clients.ClientType {
	for _, c := range clients.CLClients {
		if c == clientName {
			return clients.ClientTypeCL
		}
	}

	for _, c := range clients.ELClients {
		if c == clientName {
			return clients.ClientTypeEL
		}
	}

	return clients.ClientTypeAll
}
