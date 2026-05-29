// Package roll provides the /roll Discord command: gated, sequential image
// rollouts across a network's nodes, resolved from cartographoor inventory and
// executed via the rollpkg engine.
package roll

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/discord/cmd/common"
	rollpkg "github.com/ethpandaops/panda-pulse/pkg/roll"
	"github.com/sirupsen/logrus"
)

const (
	optionNetwork = "network"
	optionClient  = "client"
	optionImage   = "image"
	optionDelay   = "delay"
	optionForce   = "force"
	optionDryRun  = "dry_run"

	autocompleteLimit = 25
	providerCacheTTL  = 30 * time.Second
	autocompleteTime  = 2 * time.Second
)

// Config configures the roll command's actuator and inventory source.
type Config struct {
	// WatchtowerToken is the bearer token for the watchtower HTTP API.
	WatchtowerToken string
	// InventoryURL overrides the cartographoor inventory base URL.
	InventoryURL string
	// BasicAuthUser and BasicAuthPass authenticate beacon health checks behind
	// nginx basic auth (the bn-* vhosts) — only used when Dora is unavailable.
	BasicAuthUser string
	BasicAuthPass string
	// DoraURL overrides the Dora health source; empty derives it from the
	// network (https://dora.<network>.ethpandaops.io).
	DoraURL string
}

// Command implements the /roll Discord slash command.
type Command struct {
	log                *logrus.Logger
	bot                common.BotContext
	cfg                Config
	provider           *rollpkg.InventoryProvider
	guildRegistrations map[string]string
}

// NewRollCommand creates the /roll command.
func NewRollCommand(log *logrus.Logger, bot common.BotContext, cfg Config) *Command {
	return &Command{
		log:      log,
		bot:      bot,
		cfg:      cfg,
		provider: rollpkg.NewInventoryProvider(cfg.InventoryURL, "https", providerCacheTTL),
	}
}

// Name returns the command name.
func (c *Command) Name() string { return "roll" }

func (c *Command) getCommandDefinition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        c.Name(),
		Description: "Roll (re-pull + restart) client images across a network — gated and sequential",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        optionNetwork,
				Description: "Network name (e.g. glamsterdam-devnet-4)",
				Type:        discordgo.ApplicationCommandOptionString,
				Required:    true,
			},
			{
				Name:         optionClient,
				Description:  "Host pattern: client/group/node, globs, ! to exclude, 'all'",
				Type:         discordgo.ApplicationCommandOptionString,
				Required:     true,
				Autocomplete: true,
			},
			{
				Name:        optionImage,
				Description: "Scope to a specific image (optional, e.g. ethpandaops/lighthouse)",
				Type:        discordgo.ApplicationCommandOptionString,
				Required:    false,
			},
			{
				Name:        optionDelay,
				Description: "Delay between hosts, e.g. 1m or 90s (default 1m)",
				Type:        discordgo.ApplicationCommandOptionString,
				Required:    false,
			},
			{
				Name:        optionForce,
				Description: "Skip health checks — force the roll even if the node is unhealthy (e.g. known-bad node)",
				Type:        discordgo.ApplicationCommandOptionBoolean,
				Required:    false,
			},
			{
				Name:        optionDryRun,
				Description: "List what would roll without triggering",
				Type:        discordgo.ApplicationCommandOptionBoolean,
				Required:    false,
			},
		},
	}
}

// Register registers the command globally.
func (c *Command) Register(session *discordgo.Session) error {
	cmd, err := session.ApplicationCommandCreate(session.State.User.ID, "", c.getCommandDefinition())
	if err != nil {
		return fmt.Errorf("failed to register roll command: %w", err)
	}

	if c.guildRegistrations == nil {
		c.guildRegistrations = make(map[string]string, 1)
	}

	c.guildRegistrations[""] = cmd.ID

	return nil
}

// RegisterWithGuild registers the command to a specific guild.
func (c *Command) RegisterWithGuild(session *discordgo.Session, guildID string) error {
	cmd, err := session.ApplicationCommandCreate(session.State.User.ID, guildID, c.getCommandDefinition())
	if err != nil {
		return fmt.Errorf("failed to register roll command to guild %s: %w", guildID, err)
	}

	if c.guildRegistrations == nil {
		c.guildRegistrations = make(map[string]string, 2)
	}

	c.guildRegistrations[guildID] = cmd.ID

	c.log.WithField("guild", guildID).Info("Registered roll command to guild")

	return nil
}

// Handle dispatches autocomplete and command interactions.
func (c *Command) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.Type {
	case discordgo.InteractionApplicationCommandAutocomplete:
		c.handleAutocomplete(s, i)
	case discordgo.InteractionApplicationCommand:
		c.handleCommand(s, i)
	}
}

func (c *Command) handleCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	if data.Name != c.Name() {
		return
	}

	if !c.hasPermission(i.Member, s, i.GuildID) {
		c.respondEphemeral(s, i, common.NoPermissionError(c.Name()).Error())

		return
	}

	if err := c.run(s, i, data); err != nil {
		c.log.WithError(err).Error("roll command failed")
	}
}

// hasPermission allows admins or any team member to roll (mirrors /build).
func (c *Command) hasPermission(member *discordgo.Member, session *discordgo.Session, guildID string) bool {
	cfg := c.bot.GetRoleConfig()

	for _, roleName := range common.GetRoleNames(member, session, guildID) {
		if cfg.AdminRoles[strings.ToLower(roleName)] {
			return true
		}

		for _, teamRoles := range cfg.ClientRoles {
			for _, teamRole := range teamRoles {
				if strings.EqualFold(teamRole, roleName) {
					return true
				}
			}
		}
	}

	return false
}

// handleAutocomplete serves dynamic suggestions for the client option, sourced
// from the selected network's inventory (groups, node names, and "all").
func (c *Command) handleAutocomplete(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()

	var network, input string

	focused := false

	for _, opt := range data.Options {
		switch opt.Name {
		case optionNetwork:
			network = opt.StringValue()
		case optionClient:
			if opt.Focused {
				focused = true
				input = opt.StringValue()
			}
		}
	}

	var choices []*discordgo.ApplicationCommandOptionChoice

	if focused && network != "" {
		ctx, cancel := context.WithTimeout(context.Background(), autocompleteTime)
		defer cancel()

		if sugg, err := c.provider.Suggest(ctx, network, input, "", autocompleteLimit); err != nil {
			c.log.WithError(err).Debug("roll autocomplete: suggest failed")
		} else {
			choices = make([]*discordgo.ApplicationCommandOptionChoice, 0, len(sugg))
			for _, tok := range sugg {
				choices = append(choices, &discordgo.ApplicationCommandOptionChoice{Name: tok, Value: tok})
			}
		}
	}

	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionApplicationCommandAutocompleteResult,
		Data: &discordgo.InteractionResponseData{Choices: choices},
	}); err != nil {
		c.log.WithError(err).Debug("Failed to respond to roll autocomplete")
	}
}

func (c *Command) respondEphemeral(s *discordgo.Session, i *discordgo.InteractionCreate, content string) {
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: content,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	}); err != nil {
		c.log.WithError(err).Error("Failed to send ephemeral response")
	}
}
