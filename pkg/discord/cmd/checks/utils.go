package checks

import (
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

// Helper to create string pointer.
func stringPtr(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}
