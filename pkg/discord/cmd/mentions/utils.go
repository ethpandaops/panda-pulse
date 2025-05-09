package mentions

import (
	"context"
	"log"

	"github.com/bwmarrin/discordgo"
)

// getClientChoices returns the choices for the client dropdown.
func (c *MentionsCommand) getClientChoices() []*discordgo.ApplicationCommandOptionChoice {
	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, len(c.bot.GetClientsService().GetAllClients()))
	for _, client := range c.bot.GetClientsService().GetAllClients() {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
			Name:  client,
			Value: client,
		})
	}

	return choices
}

// getNetworkChoices returns the choices for the network dropdown.
func (c *MentionsCommand) getNetworkChoices() []*discordgo.ApplicationCommandOptionChoice {
	networks, err := c.bot.GetGrafana().GetNetworks(context.Background())
	if err != nil {
		log.Printf("Failed to get networks from Grafana: %v", err)

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
