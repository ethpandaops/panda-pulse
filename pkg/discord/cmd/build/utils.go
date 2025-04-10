package build

import (
	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/clients"
)

// getClientChoices returns the choices for client selection.
func (c *BuildCommand) getClientChoices() []*discordgo.ApplicationCommandOptionChoice {
	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0)

	// Add consensus clients
	for _, client := range clients.CLClients {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
			Name:  client,
			Value: client,
		})
	}

	// Add execution clients
	for _, client := range clients.ELClients {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
			Name:  client,
			Value: client,
		})
	}

	return choices
}

// stringPtr returns a pointer to the given string.
func stringPtr(s string) *string {
	return &s
}
