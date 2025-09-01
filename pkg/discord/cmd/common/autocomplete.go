package common

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/sirupsen/logrus"
)

// AutocompleteHandler handles network autocomplete for Discord commands.
type AutocompleteHandler struct {
	bot BotContext
	log *logrus.Logger
}

// NewAutocompleteHandler creates a new autocomplete handler.
func NewAutocompleteHandler(bot BotContext, log *logrus.Logger) *AutocompleteHandler {
	return &AutocompleteHandler{
		bot: bot,
		log: log,
	}
}

// HandleNetworkAutocomplete handles autocomplete for network selection.
// It returns active networks first (alphabetically sorted), followed by inactive networks.
func (h *AutocompleteHandler) HandleNetworkAutocomplete(s *discordgo.Session, i *discordgo.InteractionCreate, commandName string) {
	data := i.ApplicationCommandData()
	if data.Name != commandName {
		return
	}

	// Find the focused option
	focusedOption := h.findFocusedOption(data.Options)
	if focusedOption == nil || focusedOption.Name != "network" {
		return
	}

	// Get the current input value
	inputValue := ""
	if focusedOption.Value != nil {
		inputValue = strings.ToLower(fmt.Sprintf("%v", focusedOption.Value))
	}

	// Build and send choices
	choices := h.buildNetworkChoices(inputValue)

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionApplicationCommandAutocompleteResult,
		Data: &discordgo.InteractionResponseData{
			Choices: choices,
		},
	})
	if err != nil {
		h.log.WithError(err).Error("Failed to respond to autocomplete")
	}
}

// findFocusedOption finds the currently focused option in the interaction data.
func (h *AutocompleteHandler) findFocusedOption(options []*discordgo.ApplicationCommandInteractionDataOption) *discordgo.ApplicationCommandInteractionDataOption {
	for _, option := range options {
		if option.Type == discordgo.ApplicationCommandOptionSubCommand {
			for _, subOption := range option.Options {
				if subOption.Focused {
					return subOption
				}
			}
		}

		if option.Focused {
			return option
		}
	}

	return nil
}

// buildNetworkChoices builds the autocomplete choices for networks.
func (h *AutocompleteHandler) buildNetworkChoices(inputValue string) []*discordgo.ApplicationCommandOptionChoice {
	// Get all networks
	activeNetworks := h.bot.GetCartographoor().GetActiveNetworks()
	inactiveNetworks := h.bot.GetCartographoor().GetInactiveNetworks()

	// Build choices - max 25 per Discord limits
	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, 25)

	// Add active networks first
	for _, network := range activeNetworks {
		if inputValue == "" || strings.Contains(strings.ToLower(network), inputValue) {
			choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
				Name:  network,
				Value: network,
			})
			if len(choices) >= 25 {
				break
			}
		}
	}

	// Add inactive networks if there's room
	if len(choices) < 25 {
		for _, network := range inactiveNetworks {
			if inputValue == "" || strings.Contains(strings.ToLower(network), inputValue) {
				choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
					Name:  fmt.Sprintf("%s (inactive)", network),
					Value: network,
				})
				if len(choices) >= 25 {
					break
				}
			}
		}
	}

	return choices
}

// HandleClientAutocomplete handles autocomplete for client selection.
func (h *AutocompleteHandler) HandleClientAutocomplete(s *discordgo.Session, i *discordgo.InteractionCreate, commandName string) {
	data := i.ApplicationCommandData()
	if data.Name != commandName {
		return
	}

	// Find the focused option
	focusedOption := h.findFocusedOption(data.Options)
	if focusedOption == nil || focusedOption.Name != "client" {
		return
	}

	// Get the current input value
	inputValue := ""
	if focusedOption.Value != nil {
		inputValue = strings.ToLower(fmt.Sprintf("%v", focusedOption.Value))
	}

	// Build and send choices
	choices := h.buildClientChoices(inputValue)

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionApplicationCommandAutocompleteResult,
		Data: &discordgo.InteractionResponseData{
			Choices: choices,
		},
	})
	if err != nil {
		h.log.WithError(err).Error("Failed to respond to client autocomplete")
	}
}

// buildClientChoices builds the autocomplete choices for clients.
func (h *AutocompleteHandler) buildClientChoices(inputValue string) []*discordgo.ApplicationCommandOptionChoice {
	// Get all clients
	clients := h.bot.GetCartographoor().GetAllClients()

	// Build choices - max 25 per Discord limits
	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, 25)

	for _, client := range clients {
		if inputValue == "" || strings.Contains(strings.ToLower(client), inputValue) {
			choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
				Name:  client,
				Value: client,
			})
			if len(choices) >= 25 {
				break
			}
		}
	}

	return choices
}
