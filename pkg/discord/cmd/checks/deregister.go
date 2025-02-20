package checks

import (
	"context"
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/store"
	"github.com/sirupsen/logrus"
)

// handleDeregister handles the '/checks deregister' command.
func (c *ChecksCommand) handleDeregister(s *discordgo.Session, i *discordgo.InteractionCreate, data *discordgo.ApplicationCommandInteractionDataOption) error {
	options := data.Options
	network := options[0].StringValue()
	var client *string
	if len(options) > 1 {
		c := options[1].StringValue()
		client = &c
	}

	c.log.WithFields(logrus.Fields{
		"command": "/checks deregister",
		"network": network,
		"user":    i.Member.User.Username,
	}).Info("Received command")

	if err := c.deregisterAlert(context.Background(), network, client); err != nil {
		if notRegistered, ok := err.(*store.AlertNotRegisteredError); ok {
			var msg string

			if notRegistered.Client == "any" {
				msg = fmt.Sprintf("ℹ️ No clients are registered for **%s** checks", network)
			} else {
				msg = fmt.Sprintf("ℹ️ Client **%s** is not registered for **%s** checks", notRegistered.Client, network)
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
		msg = fmt.Sprintf("✅ Successfully deregistered **%s** from **%s** notifications", *client, network)
	} else {
		msg = fmt.Sprintf("✅ Successfully deregistered **all clients** from **%s** notifications", network)
	}

	return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: msg,
		},
	})
}

// deregisterAlert deregisters an alert for a given network and client.
func (c *ChecksCommand) deregisterAlert(ctx context.Context, network string, client *string) error {
	alerts, err := c.bot.GetMonitorRepo().List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list alerts: %w", err)
	}

	// If client is specified, only remove that client's alert.
	if client != nil {
		// Find the alert monitor in question.
		var alert *store.MonitorAlert
		for _, a := range alerts {
			if a.Network == network && a.Client == *client {
				alert = a

				break
			}
		}

		if alert == nil {
			return &store.AlertNotRegisteredError{
				Network: network,
				Client:  *client,
			}
		}

		if err := c.unscheduleAlert(ctx, alert); err != nil {
			return fmt.Errorf("failed to unschedule alert: %w", err)
		}

		return nil
	}

	var found bool

	// Otherwise, remove all clients for this network.
	for _, alert := range alerts {
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
			Client:  "any",
		}
	}

	return nil
}

func (c *ChecksCommand) unscheduleAlert(ctx context.Context, alert *store.MonitorAlert) error {
	key := c.bot.GetMonitorRepo().Key(alert)

	// Remove from S3.
	if err := c.bot.GetMonitorRepo().Purge(ctx, alert.Network, alert.Client); err != nil {
		return fmt.Errorf("failed to delete alert: %w", err)
	}

	c.log.WithFields(logrus.Fields{
		"network": alert.Network,
		"channel": alert.DiscordChannel,
		"client":  alert.Client,
		"key":     key,
	}).Info("Deregistered monitor")

	// Remove from scheduler.
	c.bot.GetScheduler().RemoveJob(key)

	c.log.WithField("key", key).Info("Unscheduled monitor alert")

	return nil
}
