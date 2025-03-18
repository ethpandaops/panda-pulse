package hive

import (
	"context"

	"github.com/bwmarrin/discordgo"
)

// getNetworkChoices returns the choices for the network dropdown.
func (c *HiveCommand) getNetworkChoices() []*discordgo.ApplicationCommandOptionChoice {
	networks, err := c.bot.GetGrafana().GetNetworks(context.Background())
	if err != nil {
		c.log.WithError(err).Error("Failed to get networks from Grafana")

		return nil
	}

	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, len(networks))
	for _, network := range networks {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
			Name:  network,
			Value: network,
		})
	}

	return choices
}

// stringPtr returns a pointer to the string value passed in.
func stringPtr(s string) *string {
	return &s
}
