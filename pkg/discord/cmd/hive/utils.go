package hive

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
)

// getNetworkChoices returns the choices for the network dropdown.
func (c *HiveCommand) getNetworkChoices() []*discordgo.ApplicationCommandOptionChoice {
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

// stringPtr returns a pointer to the string value passed in.
func stringPtr(s string) *string {
	return &s
}
