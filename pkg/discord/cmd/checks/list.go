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

const (
	msgNoChecksRegistered = "ℹ️ No checks are currently registered%s\n"
	msgNoChecksForNetwork = " for the network **%s**"
	msgNoChecksAnyNetwork = " for any network"
	msgNetworkClients     = "🌐 Clients registered for **%s** notifications\n"
	msgAlertsSentTo       = "Alerts are sent to "
)

// clientInfo represents registration status and channel for a client.
type clientInfo struct {
	registered bool
	channelID  string
}

// handleList handles the '/checks list' command.
func (c *ChecksCommand) handleList(
	s *discordgo.Session,
	i *discordgo.InteractionCreate,
	data *discordgo.ApplicationCommandInteractionDataOption,
) error {
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

	// Get all unique networks.
	networks := make(map[string]bool)
	for _, alert := range alerts {
		networks[alert.Network] = true
	}

	// If no alerts found.
	if len(networks) == 0 {
		suffix := msgNoChecksAnyNetwork

		if network != nil {
			suffix = fmt.Sprintf(msgNoChecksForNetwork, *network)
		}

		return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf(msgNoChecksRegistered, suffix),
			},
		})
	}

	// First, respond to the interaction to acknowledge it.
	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "Listing checks...",
		},
	})
	if err != nil {
		return fmt.Errorf("failed to respond to interaction: %w", err)
	}

	// Then send each network's table as a separate message, we do this to get around the 2000 message limit.
	for networkName := range networks {
		if network != nil && networkName != *network {
			continue
		}

		// Create a map of registered clients for this network.
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

		var msg strings.Builder

		msg.WriteString(fmt.Sprintf(msgNetworkClients, networkName))
		msg.WriteString(buildClientTable(allClients, registered))

		// Collect all unique channels.
		channels := make(map[string]bool)

		for _, alert := range alerts {
			if alert.Network == networkName {
				channels[alert.DiscordChannel] = true
			}
		}

		if len(channels) > 0 {
			msg.WriteString(msgAlertsSentTo)

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

		if _, err := s.ChannelMessageSend(i.ChannelID, msg.String()); err != nil {
			c.log.WithError(err).WithField("network", networkName).Error("Failed to send network checks table")
		}
	}

	return nil
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

// buildClientTable creates an ASCII table of client statuses.
func buildClientTable(clients []string, registered map[string]clientInfo) string {
	var msg strings.Builder

	msg.WriteString("```\n")
	msg.WriteString("┌──────────────┬────────┐\n")
	msg.WriteString("│ Client       │ Status │\n")
	msg.WriteString("├──────────────┼────────┤\n")

	for _, client := range clients {
		info := registered[client]
		status := "❌"

		if info.registered {
			status = "✅"
		}

		msg.WriteString(fmt.Sprintf("│ %-12s │   %s   │\n", client, status))
	}

	msg.WriteString("└──────────────┴────────┘\n```")

	return msg.String()
}
