package checks

import (
	"fmt"

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
func (c *ChecksCommand) getNetworkChoices() []*discordgo.ApplicationCommandOptionChoice {
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

// Helper to create string pointer.
func stringPtr(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}
