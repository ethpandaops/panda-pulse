package mentions

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
)

// getClientChoices returns the choices for the client dropdown.
func (c *MentionsCommand) getClientChoices() []*discordgo.ApplicationCommandOptionChoice {
	var (
		clients = c.bot.GetCartographoor().GetAllClients()
		choices = make([]*discordgo.ApplicationCommandOptionChoice, 0, len(clients))
	)

	for _, client := range clients {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
			Name:  client,
			Value: client,
		})
	}

	return choices
}

// getNetworkChoices returns the choices for the network dropdown.
func (c *MentionsCommand) getNetworkChoices() []*discordgo.ApplicationCommandOptionChoice {
	var (
		activeNetworks   = c.bot.GetCartographoor().GetActiveNetworks()
		inactiveNetworks = c.bot.GetCartographoor().GetInactiveNetworks()
		choices          = make([]*discordgo.ApplicationCommandOptionChoice, 0, len(activeNetworks)+len(inactiveNetworks))
	)

	for _, network := range activeNetworks {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
			Name:  network,
			Value: network,
		})
	}

	for _, network := range inactiveNetworks {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
			Name:  fmt.Sprintf("%s (inactive)", network),
			Value: network,
		})
	}

	return choices
}
