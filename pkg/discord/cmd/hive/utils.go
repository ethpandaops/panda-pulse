package hive

import (
	"github.com/bwmarrin/discordgo"
)

// getNetworkChoices returns the choices for the network dropdown.
func (c *HiveCommand) getNetworkChoices() []*discordgo.ApplicationCommandOptionChoice {
	var (
		networks = c.bot.GetCartographoor().GetActiveNetworks()
		choices  = make([]*discordgo.ApplicationCommandOptionChoice, 0, len(networks))
	)

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
