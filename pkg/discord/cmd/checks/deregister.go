package checks

import (
	"context"
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/store"
	"github.com/sirupsen/logrus"
)

const (
	msgNoClientsRegistered = "ℹ️ No clients are registered for **%s** checks"
	msgClientNotRegistered = "ℹ️ Client **%s** is not registered for **%s** checks"
	msgDeregisteredClient  = "✅ Successfully deregistered **%s** from **%s** notifications"
	msgDeregisteredAll     = "✅ Successfully deregistered **all clients** from **%s** notifications"
)

// handleDeregister handles the '/checks deregister' command.
func (c *ChecksCommand) handleDeregister(
	s *discordgo.Session,
	i *discordgo.InteractionCreate,
	data *discordgo.ApplicationCommandInteractionDataOption,
) error {
	var (
		options = data.Options
		network = options[0].StringValue()
		client  *string
		guildID = i.GuildID // Get the guild ID from the interaction
	)

	if len(options) > 1 {
		c := options[1].StringValue()
		client = &c
	}

	c.log.WithFields(logrus.Fields{
		"command": "/checks deregister",
		"network": network,
		"guild":   guildID,
		"user":    i.Member.User.Username,
	}).Info("Received command")

	if err := c.deregisterAlert(context.Background(), network, guildID, client); err != nil {
		if notRegistered, ok := err.(*store.AlertNotRegisteredError); ok {
			msg := fmt.Sprintf(msgClientNotRegistered, notRegistered.Client, network)

			if notRegistered.Client == "any" {
				msg = fmt.Sprintf(msgNoClientsRegistered, network)
			}

			return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: msg,
				},
			})
		}

		return fmt.Errorf("failed to deregister alert: %w", err)
	}

	var msg string

	if client != nil {
		msg = fmt.Sprintf(msgDeregisteredClient, *client, network)
	} else {
		msg = fmt.Sprintf(msgDeregisteredAll, network)
	}

	return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: msg,
		},
	})
}

// deregisterAlert deregisters an alert for a given network and client.
func (c *ChecksCommand) deregisterAlert(ctx context.Context, network, guildID string, client *string) error {
	// First, list all alerts.
	alerts, err := c.bot.GetMonitorRepo().List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list alerts: %w", err)
	}

	// Filter alerts for this guild
	guildAlerts := make([]*store.MonitorAlert, 0)

	for _, alert := range alerts {
		if alert.DiscordGuildID == guildID {
			guildAlerts = append(guildAlerts, alert)
		}
	}

	// If client is specified, only remove that client's alert.
	if client != nil {
		alert := c.getExistingAlert(guildAlerts, network, *client)
		if alert == nil {
			return &store.AlertNotRegisteredError{
				Network: network,
				Guild:   guildID,
				Client:  *client,
			}
		}

		if err := c.unscheduleAlert(ctx, alert); err != nil {
			return fmt.Errorf("failed to unschedule alert: %w", err)
		}

		return nil
	}

	// Otherwise, remove all clients for this network.
	var found bool

	for _, alert := range guildAlerts {
		if alert.Network == network {
			found = true

			if err := c.unscheduleAlert(ctx, alert); err != nil {
				return fmt.Errorf("failed to unschedule alert: %w", err)
			}
		}
	}

	if !found {
		return &store.AlertNotRegisteredError{
			Network: network,
			Guild:   guildID,
			Client:  "any",
		}
	}

	return nil
}

func (c *ChecksCommand) unscheduleAlert(ctx context.Context, alert *store.MonitorAlert) error {
	key := c.bot.GetMonitorRepo().Key(alert)

	// Remove from S3
	if err := c.bot.GetMonitorRepo().Purge(ctx, alert.Network, alert.Client); err != nil {
		return fmt.Errorf("failed to delete alert: %w", err)
	}

	c.log.WithFields(logrus.Fields{
		"network": alert.Network,
		"channel": alert.DiscordChannel,
		"client":  alert.Client,
		"key":     key,
	}).Info("Deregistered monitor")

	// Remove from scheduler
	c.bot.GetScheduler().RemoveJob(key)

	c.log.WithField("key", key).Info("Unscheduled monitor alert")

	return nil
}

// getExistingAlert finds an alert for a given network and client.
func (c *ChecksCommand) getExistingAlert(alerts []*store.MonitorAlert, network, client string) *store.MonitorAlert {
	for _, a := range alerts {
		if a.Network == network && a.Client == client {
			return a
		}
	}

	return nil
}
