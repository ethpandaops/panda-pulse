// Package rollouts posts the rollouts rolloor runs on a devnet to a channel,
// for the devnets that run rolloor.
package rollouts

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/sirupsen/logrus"

	"github.com/ethpandaops/panda-pulse/pkg/discord/cmd/common"
	"github.com/ethpandaops/panda-pulse/pkg/rolloor"
	"github.com/ethpandaops/panda-pulse/pkg/store"
)

const (
	commandName   = "rollouts"
	optionNetwork = "network"
	optionChannel = "channel"

	// pollInterval is how often each registered network's history is read.
	pollInterval = 30 * time.Second
	// pageSize bounds how many events one poll reads per network.
	pageSize = 200
)

// alertStore is what the command needs from the rollouts repository.
type alertStore interface {
	List(ctx context.Context) ([]*store.RolloutAlert, error)
	Get(ctx context.Context, network, guildID string) (*store.RolloutAlert, error)
	Persist(ctx context.Context, alert *store.RolloutAlert) error
	Purge(ctx context.Context, identifiers ...string) error
}

// history is what the command needs from rolloor.
type history interface {
	Available(ctx context.Context, network string) bool
	History(ctx context.Context, network string, after int64, limit int) ([]rolloor.Event, error)
	Latest(ctx context.Context, network string) (int64, error)
	URL(network string) string
}

// poster sends a message to a channel; *discordgo.Session is one.
type poster interface {
	ChannelMessageSendComplex(channelID string, data *discordgo.MessageSend, options ...discordgo.RequestOption) (*discordgo.Message, error)
}

// mentionSource finds who to ping for a client on a network.
type mentionSource interface {
	Get(ctx context.Context, network, client, guildID string) (*store.ClientMention, error)
}

// RolloutsCommand is /rollouts and the watcher behind it.
type RolloutsCommand struct {
	log     logrus.FieldLogger
	bot     common.BotContext
	alerts  alertStore
	rolloor history

	// post, mentions and activeNetworks default to the bot's session,
	// mentions repo and cartographoor.
	post           func() poster
	mentions       func() mentionSource
	activeNetworks func() []string

	registrations map[string]string
	stop          chan struct{}
	wg            sync.WaitGroup
}

// NewRolloutsCommand creates the command.
func NewRolloutsCommand(log logrus.FieldLogger, bot common.BotContext, alerts *store.RolloutsRepo, client *rolloor.Client) *RolloutsCommand {
	return &RolloutsCommand{
		log:            log.WithField("command", commandName),
		bot:            bot,
		alerts:         alerts,
		rolloor:        client,
		post:           func() poster { return bot.GetSession() },
		mentions:       func() mentionSource { return bot.GetMentionsRepo() },
		activeNetworks: func() []string { return bot.GetCartographoor().GetActiveNetworks() },
		registrations:  map[string]string{},
	}
}

// Name returns the command name.
func (c *RolloutsCommand) Name() string {
	return commandName
}

func (c *RolloutsCommand) definition() *discordgo.ApplicationCommand {
	network := func(desc string) *discordgo.ApplicationCommandOption {
		return &discordgo.ApplicationCommandOption{
			Name: optionNetwork, Description: desc, Type: discordgo.ApplicationCommandOptionString, Required: true, Autocomplete: true,
		}
	}

	return &discordgo.ApplicationCommand{
		Name:        commandName,
		Description: "Post a devnet's rolloor rollouts to a channel",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "register",
				Description: "Post this devnet's rollouts and halt alerts to a channel",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					network("A devnet that runs rolloor"),
					{
						Name: optionChannel, Description: "Channel to post to", Type: discordgo.ApplicationCommandOptionChannel, Required: true,
						ChannelTypes: []discordgo.ChannelType{discordgo.ChannelTypeGuildText},
					},
				},
			},
			{
				Name:        "deregister",
				Description: "Stop posting this devnet's rollouts",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options:     []*discordgo.ApplicationCommandOption{network("The devnet")},
			},
			{
				Name:        "list",
				Description: "List the devnets whose rollouts are posted here",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
			},
		},
	}
}

// Register registers the command globally.
func (c *RolloutsCommand) Register(session *discordgo.Session) error {
	cmd, err := session.ApplicationCommandCreate(session.State.User.ID, "", c.definition())
	if err != nil {
		return fmt.Errorf("failed to register rollouts command: %w", err)
	}

	c.registrations[""] = cmd.ID

	return nil
}

// RegisterWithGuild registers the command with one guild.
func (c *RolloutsCommand) RegisterWithGuild(session *discordgo.Session, guildID string) error {
	cmd, err := session.ApplicationCommandCreate(session.State.User.ID, guildID, c.definition())
	if err != nil {
		return fmt.Errorf("failed to register rollouts command to guild %s: %w", guildID, err)
	}

	c.registrations[guildID] = cmd.ID

	return nil
}

// Handle answers autocomplete and runs subcommands.
func (c *RolloutsCommand) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	if data.Name != commandName || len(data.Options) == 0 {
		return
	}

	sub := data.Options[0]

	if i.Type == discordgo.InteractionApplicationCommandAutocomplete {
		c.respond(s, i, &discordgo.InteractionResponse{
			Type: discordgo.InteractionApplicationCommandAutocompleteResult,
			Data: &discordgo.InteractionResponseData{Choices: c.networkChoices(context.Background(), focusedValue(sub))},
		})

		return
	}

	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	var reply string

	switch sub.Name {
	case "register":
		reply = c.register(context.Background(), i.GuildID, stringOption(sub, optionNetwork), channelOption(s, sub))
	case "deregister":
		reply = c.deregister(context.Background(), i.GuildID, stringOption(sub, optionNetwork))
	case "list":
		reply = c.list(context.Background(), i.GuildID)
	default:
		return
	}

	c.respond(s, i, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: reply, Flags: discordgo.MessageFlagsEphemeral},
	})
}

func (c *RolloutsCommand) respond(s *discordgo.Session, i *discordgo.InteractionCreate, resp *discordgo.InteractionResponse) {
	if err := s.InteractionRespond(i.Interaction, resp); err != nil {
		c.log.WithError(err).Error("Failed to respond")
	}
}

// networkChoices offers the active devnets that run rolloor. Discord waits
// three seconds for an answer, so probes run together under a deadline.
func (c *RolloutsCommand) networkChoices(ctx context.Context, typed string) []*discordgo.ApplicationCommandOptionChoice {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	var matching []string

	for _, name := range c.activeNetworks() {
		if strings.Contains(name, strings.ToLower(typed)) {
			matching = append(matching, name)
		}
	}

	names := c.runningRolloor(ctx, matching)
	sort.Strings(names)

	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, min(len(names), 25))
	for _, n := range names[:min(len(names), 25)] {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{Name: n, Value: n})
	}

	return choices
}

// runningRolloor returns the networks that answer as rolloor, asking them
// all at once.
func (c *RolloutsCommand) runningRolloor(ctx context.Context, networks []string) []string {
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		out []string
	)

	for _, name := range networks {
		wg.Add(1)

		go func() {
			defer wg.Done()

			if !c.rolloor.Available(ctx, name) {
				return
			}

			mu.Lock()
			defer mu.Unlock()

			out = append(out, name)
		}()
	}

	wg.Wait()

	return out
}

// register starts posting a network's rollouts from its newest event on, so
// nothing already past is posted.
func (c *RolloutsCommand) register(ctx context.Context, guildID, network string, channel *discordgo.Channel) string {
	if channel == nil {
		return "🚫 Pick a text channel"
	}

	if !c.rolloor.Available(ctx, network) {
		return fmt.Sprintf("🚫 rolloor is not running on **%s**", network)
	}

	latest, err := c.rolloor.Latest(ctx, network)
	if err != nil {
		return fmt.Sprintf("🚫 Could not read rolloor on **%s**: %v", network, err)
	}

	now := time.Now()
	alert := &store.RolloutAlert{Network: network, DiscordGuildID: guildID, DiscordChannel: channel.ID, LastEventID: latest, CreatedAt: now, UpdatedAt: now}

	if existing, err := c.alerts.Get(ctx, network, guildID); err == nil {
		alert.CreatedAt = existing.CreatedAt
	}

	if err := c.alerts.Persist(ctx, alert); err != nil {
		return fmt.Sprintf("🚫 Could not save: %v", err)
	}

	return fmt.Sprintf("✅ Posting **%s** rollouts to <#%s>", network, channel.ID)
}

func (c *RolloutsCommand) deregister(ctx context.Context, guildID, network string) string {
	if _, err := c.alerts.Get(ctx, network, guildID); err != nil {
		if errors.Is(err, store.ErrRolloutAlertNotFound) {
			return fmt.Sprintf("ℹ️ **%s** rollouts are not posted here", network)
		}

		return fmt.Sprintf("🚫 Could not read: %v", err)
	}

	if err := c.alerts.Purge(ctx, network, guildID); err != nil {
		return fmt.Sprintf("🚫 Could not remove: %v", err)
	}

	return fmt.Sprintf("✅ Stopped posting **%s** rollouts", network)
}

func (c *RolloutsCommand) list(ctx context.Context, guildID string) string {
	alerts, err := c.alerts.List(ctx)
	if err != nil {
		return fmt.Sprintf("🚫 Could not read: %v", err)
	}

	var lines []string

	for _, a := range alerts {
		if a.DiscordGuildID == guildID {
			lines = append(lines, fmt.Sprintf("• **%s** in <#%s>", a.Network, a.DiscordChannel))
		}
	}

	if len(lines) == 0 {
		return "ℹ️ No devnet's rollouts are posted here"
	}

	sort.Strings(lines)

	return strings.Join(lines, "\n")
}

func stringOption(sub *discordgo.ApplicationCommandInteractionDataOption, name string) string {
	for _, o := range sub.Options {
		if o.Name == name {
			return o.StringValue()
		}
	}

	return ""
}

func channelOption(s *discordgo.Session, sub *discordgo.ApplicationCommandInteractionDataOption) *discordgo.Channel {
	for _, o := range sub.Options {
		if o.Name == optionChannel {
			return o.ChannelValue(s)
		}
	}

	return nil
}

func focusedValue(sub *discordgo.ApplicationCommandInteractionDataOption) string {
	for _, o := range sub.Options {
		if o.Focused {
			return fmt.Sprint(o.Value)
		}
	}

	return ""
}
