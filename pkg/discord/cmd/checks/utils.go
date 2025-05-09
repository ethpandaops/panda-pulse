package checks

import (
	"context"
	"log"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/checks"
)

// categoryResults is a struct that holds the results of a category.
type categoryResults struct {
	failedChecks []*checks.Result
	hasFailed    bool
}

// Order categories as we want them to be displayed.
var orderedCategories = []checks.Category{
	checks.CategoryGeneral,
	checks.CategorySync,
}

// getClientChoices returns the choices for the client dropdown.
func (c *ChecksCommand) getClientChoices() []*discordgo.ApplicationCommandOptionChoice {
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
func (c *ChecksCommand) getNetworkChoices() []*discordgo.ApplicationCommandOptionChoice {
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

// Helper to create string pointer.
func stringPtr(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}
