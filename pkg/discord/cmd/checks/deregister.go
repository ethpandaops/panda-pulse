package checks

import (
	"context"
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/clients"
	"github.com/ethpandaops/panda-pulse/pkg/store"
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

	c.log.Infof("Received checks deregister command: network=%s client=%v from user=%s",
		network, client, i.Member.User.Username)

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
	c.log.Infof("Deregistering alert for network=%s client=%v", network, client)

	// If client is specified, only remove that client's alert.
	if client != nil {
		// First try to find the alert to get its type.
		alerts, err := c.bot.GetMonitorRepo().List(ctx)
		if err != nil {
			return fmt.Errorf("failed to list alerts: %w", err)
		}

		// Find the alert to get its type.
		var (
			clientType clients.ClientType
			found      bool
		)

		for _, a := range alerts {
			if a.Network == network && a.Client == *client {
				clientType = a.ClientType
				found = true

				break
			}
		}

		if !found {
			return &store.AlertNotRegisteredError{
				Network: network,
				Client:  *client,
			}
		}

		jobName := fmt.Sprintf("monitor-alert-%s-%s-%s", network, clientType, *client)
		c.bot.GetScheduler().RemoveJob(jobName)

		// Remove from S3.
		if err := c.bot.GetMonitorRepo().Purge(ctx, network, *client); err != nil {
			return fmt.Errorf("failed to delete alert: %w", err)
		}

		return nil
	}

	// Otherwise, remove all clients for this network.
	alerts, err := c.bot.GetMonitorRepo().List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list alerts: %w", err)
	}

	found := false

	for _, alert := range alerts {
		if alert.Network == network {
			found = true
			jobName := fmt.Sprintf("monitor-alert-%s-%s-%s", network, alert.ClientType, alert.Client)
			c.bot.GetScheduler().RemoveJob(jobName)

			if err := c.bot.GetMonitorRepo().Purge(ctx, network, alert.Client); err != nil {
				return fmt.Errorf("failed to delete alert: %w", err)
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
