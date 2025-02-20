package checks

import (
	"context"
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/clients"
	"github.com/ethpandaops/panda-pulse/pkg/store"
	"github.com/sirupsen/logrus"
)

// handleRegister handles the '/checks register' command.
func (c *ChecksCommand) handleRegister(s *discordgo.Session, i *discordgo.InteractionCreate, data *discordgo.ApplicationCommandInteractionDataOption) error {
	var (
		options = data.Options
		network = options[0].StringValue()
		channel = options[1].ChannelValue(s)
		client  *string
	)

	if len(options) > 2 {
		c := options[2].StringValue()
		client = &c
	}

	c.log.WithFields(logrus.Fields{
		"command": "/checks register",
		"network": network,
		"channel": channel.Name,
		"user":    i.Member.User.Username,
	}).Info("Received command")

	if err := c.registerAlert(context.Background(), network, channel.ID, client); err != nil {
		if alreadyRegistered, ok := err.(*store.AlertAlreadyRegisteredError); ok {
			msg := fmt.Sprintf("ℹ️ Client **%s** is already registered for **%s** in <#%s>",
				alreadyRegistered.Client, network, channel.ID)

			return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: msg,
				},
			})
		}

		return fmt.Errorf("failed to register alert: %w", err)
	}

	var msg string

	if client != nil {
		msg = fmt.Sprintf("✅ Successfully registered **%s** for **%s** notifications in <#%s>", *client, network, channel.ID)
	} else {
		msg = fmt.Sprintf("✅ Successfully registered **all clients** for **%s** notifications in <#%s>", network, channel.ID)
	}

	return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: msg,
		},
	})
}

func (c *ChecksCommand) registerAlert(ctx context.Context, network, channelID string, specificClient *string) error {
	if specificClient == nil {
		// For registering all clients, just proceed with registration.
		return c.registerAllClients(ctx, network, channelID)
	}

	// Check if this specific client is already registered.
	alerts, err := c.bot.GetMonitorRepo().List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list alerts: %w", err)
	}

	for _, alert := range alerts {
		if alert.Network == network && alert.Client == *specificClient && alert.DiscordChannel == channelID {
			return &store.AlertAlreadyRegisteredError{
				Network: network,
				Channel: channelID,
				Client:  *specificClient,
			}
		}
	}

	// Check if client exists in our known clients.
	clientType := clients.ClientTypeAll

	for _, c := range clients.CLClients {
		if c == *specificClient {
			clientType = clients.ClientTypeCL

			break
		}
	}

	if clientType == clients.ClientTypeAll {
		for _, c := range clients.ELClients {
			if c == *specificClient {
				clientType = clients.ClientTypeEL

				break
			}
		}
	}

	if clientType == clients.ClientTypeAll {
		return fmt.Errorf("unknown client: %s", *specificClient)
	}

	alert := &store.MonitorAlert{
		Network:        network,
		Client:         *specificClient,
		ClientType:     clientType,
		DiscordChannel: channelID,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	if err := c.scheduleAlert(ctx, alert); err != nil {
		return fmt.Errorf("failed to schedule alert: %w", err)
	}

	return nil
}

// registerAllClients registers a monitor alert for all clients for a given network.
func (c *ChecksCommand) registerAllClients(ctx context.Context, network, channelID string) error {
	// Register CL clients.
	for _, client := range clients.CLClients {
		alert := &store.MonitorAlert{
			Network:        network,
			Client:         client,
			ClientType:     clients.ClientTypeCL,
			DiscordChannel: channelID,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}

		if err := c.scheduleAlert(ctx, alert); err != nil {
			return fmt.Errorf("failed to schedule CL alert: %w", err)
		}
	}

	// Register EL clients.
	for _, client := range clients.ELClients {
		alert := &store.MonitorAlert{
			Network:        network,
			Client:         client,
			ClientType:     clients.ClientTypeEL,
			DiscordChannel: channelID,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}

		if err := c.scheduleAlert(ctx, alert); err != nil {
			return fmt.Errorf("failed to schedule EL alert: %w", err)
		}
	}

	return nil
}

// scheduleAlert schedules a monitor alert to run every minute.
func (c *ChecksCommand) scheduleAlert(ctx context.Context, alert *store.MonitorAlert) error {
	if err := c.bot.GetMonitorRepo().Persist(ctx, alert); err != nil {
		c.log.WithError(err).Error("Failed to persist alert")

		return err
	}

	var (
		schedule = "*/1 * * * *"
		jobName  = c.bot.GetMonitorRepo().Key(alert)
	)

	c.log.WithFields(logrus.Fields{
		"network": alert.Network,
		"channel": alert.DiscordChannel,
		"client":  alert.Client,
		"key":     jobName,
	}).Info("Registered monitor")

	if err := c.bot.GetScheduler().AddJob(jobName, schedule, func(ctx context.Context) error {
		c.log.Infof("Running checks for network=%s client=%s", alert.Network, alert.Client)

		_, err := c.RunChecks(ctx, alert)

		return err
	}); err != nil {
		return fmt.Errorf("failed to schedule alert: %w", err)
	}

	c.log.WithFields(logrus.Fields{
		"schedule": schedule,
		"key":      jobName,
	}).Info("Scheduled monitor alert")

	return nil
}
