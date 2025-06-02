package mentions

import (
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
