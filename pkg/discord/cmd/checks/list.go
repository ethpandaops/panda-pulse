package checks

import (
	"context"
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/clients"
	"github.com/ethpandaops/panda-pulse/pkg/store"
	"github.com/sirupsen/logrus"
)

// handleList handles the '/checks list' command.
func (c *ChecksCommand) handleList(s *discordgo.Session, i *discordgo.InteractionCreate, data *discordgo.ApplicationCommandInteractionDataOption) error {
	var network *string

	if len(data.Options) > 0 {
		n := data.Options[0].StringValue()
		network = &n
	}

	c.log.WithFields(logrus.Fields{
		"command": "/checks list",
		"user":    i.Member.User.Username,
	}).Info("Received command")

	alerts, err := c.listAlerts(context.Background(), network)
	if err != nil {
		return fmt.Errorf("failed to list alerts: %w", err)
	}

	var msg strings.Builder

	// Get all unique networks.
	networks := make(map[string]bool)
	for _, alert := range alerts {
		networks[alert.Network] = true
	}

	// If no alerts found.
	if len(networks) == 0 {
		msg.WriteString("ℹ️ No checks are currently registered")

		if network != nil {
			msg.WriteString(fmt.Sprintf(" for the network **%s**", *network))
		} else {
			msg.WriteString(" for any network")
		}

		msg.WriteString("\n")
	}

	// For each network, show the client status table.
	for networkName := range networks {
		if network != nil && networkName != *network {
			continue
		}

		// Create a map of registered clients for this network.
		type clientInfo struct {
			registered bool
			channelID  string
		}

		var (
			registered = make(map[string]clientInfo)
			allClients = append(clients.CLClients, clients.ELClients...)
		)

		// Initialize all clients as unregistered.
		for _, client := range allClients {
			registered[client] = clientInfo{registered: false}
		}

		// Update with registered clients and their channels.
		for _, alert := range alerts {
			if alert.Network == networkName {
				registered[alert.Client] = clientInfo{
					registered: true,
					channelID:  alert.DiscordChannel,
				}
			}
		}

		msg.WriteString(fmt.Sprintf("🌐 Clients registered for **%s** notifications\n", networkName))
		msg.WriteString("```\n")
		msg.WriteString("┌──────────────┬────────┐\n")
		msg.WriteString("│ Client       │ Status │\n")
		msg.WriteString("├──────────────┼────────┤\n")

		for _, client := range allClients {
			var (
				info   = registered[client]
				status = "❌"
			)

			if info.registered {
				status = "✅"
			}

			msg.WriteString(fmt.Sprintf("│ %-12s │   %s   │\n", client, status))
		}

		msg.WriteString("└──────────────┴────────┘\n```")

		// Collect all unique channels.
		channels := make(map[string]bool)

		for _, alert := range alerts {
			if alert.Network == networkName {
				channels[alert.DiscordChannel] = true
			}
		}

		if len(channels) > 0 {
			msg.WriteString("Alerts are sent to ")

			var first = true

			for channelID := range channels {
				if !first {
					msg.WriteString(", ")
				}

				msg.WriteString(fmt.Sprintf("<#%s>", channelID))

				first = false
			}

			msg.WriteString("\n")
		}
	}

	return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: msg.String(),
		},
	})
}

// listAlerts lists all alerts for a given network.
func (c *ChecksCommand) listAlerts(ctx context.Context, network *string) ([]*store.MonitorAlert, error) {
	alerts, err := c.bot.GetMonitorRepo().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list alerts: %w", err)
	}

	if network == nil {
		return alerts, nil
	}

	// Filter alerts for specific network.
	filtered := make([]*store.MonitorAlert, 0)

	for _, alert := range alerts {
		if alert.Network == *network {
			filtered = append(filtered, alert)
		}
	}

	return filtered, nil
}
