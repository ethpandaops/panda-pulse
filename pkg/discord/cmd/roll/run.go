package roll

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	rollpkg "github.com/ethpandaops/panda-pulse/pkg/roll"
)

// minEditInterval throttles progress edits to avoid Discord rate limits.
const minEditInterval = time.Second

// run resolves targets, posts a live progress message to the channel, and drives
// the gated rollout. Progress is tracked in a normal bot message (not the
// interaction reply) so it survives past Discord's 15-minute interaction-token
// window — a multi-node roll can take longer than that.
func (c *Command) run(s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) error {
	var (
		network, client, image, delayStr string
		dryRun, force                    bool
	)

	for _, opt := range data.Options {
		switch opt.Name {
		case optionNetwork:
			network = opt.StringValue()
		case optionClient:
			client = opt.StringValue()
		case optionImage:
			image = opt.StringValue()
		case optionDelay:
			delayStr = opt.StringValue()
		case optionForce:
			force = opt.BoolValue()
		case optionDryRun:
			dryRun = opt.BoolValue()
		}
	}

	var delay time.Duration

	if delayStr != "" {
		d, err := time.ParseDuration(delayStr)
		if err != nil {
			c.respondEphemeral(s, i, fmt.Sprintf("❌ Invalid delay %q: %v", delayStr, err))

			return nil
		}

		delay = d
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	targets, err := c.provider.Targets(ctx, network)
	if err != nil {
		c.respondEphemeral(s, i, fmt.Sprintf("❌ Failed to load inventory for **%s**: %v", network, err))

		return nil
	}

	targets = rollpkg.Select(targets, client)
	if len(targets) == 0 {
		c.respondEphemeral(s, i, fmt.Sprintf("❌ No hosts matched `%s` on **%s**.", client, network))

		return nil
	}

	actuator, err := c.actuator()
	if err != nil {
		c.respondEphemeral(s, i, fmt.Sprintf("❌ Roll not configured: %v", err))

		return nil
	}

	// Ack the slash command immediately; live progress goes to a channel message
	// (editable indefinitely, unlike the 15-minute interaction token).
	c.respondEphemeral(s, i, fmt.Sprintf("🚀 Roll started for %d host(s) on **%s** — tracking in this channel.", len(targets), network))

	ui := newRollUI(network, client, image, dryRun, targets)

	msg, err := s.ChannelMessageSend(i.ChannelID, ui.render())
	if err != nil {
		return fmt.Errorf("failed to post progress message: %w", err)
	}

	var lastEdit time.Time

	edit := func(force bool) {
		if !force && time.Since(lastEdit) < minEditInterval {
			return
		}

		lastEdit = time.Now()

		if _, e := s.ChannelMessageEdit(i.ChannelID, msg.ID, ui.render()); e != nil {
			c.log.WithError(e).Debug("roll: failed to edit progress message")
		}
	}

	doraURL := c.cfg.DoraURL
	if doraURL == "" {
		doraURL = rollpkg.DoraURLForNetwork(network)
	}

	runErr := rollpkg.NewEngine(actuator, c.log).Run(ctx, targets, rollpkg.Options{
		Image:               image,
		DryRun:              dryRun,
		SkipHealth:          force,
		DelayBetweenNodes:   delay,
		DoraURL:             doraURL,
		BeaconBasicAuthUser: c.cfg.BasicAuthUser,
		BeaconBasicAuthPass: c.cfg.BasicAuthPass,
		OnProgress: func(p rollpkg.Progress) {
			ui.update(p)
			edit(false)
		},
	})

	ui.finish(runErr)
	edit(true)

	c.notify(s, i, network, len(targets), runErr)

	return nil
}

// notify pings the invoking user with the roll outcome (a new message, so it
// actually notifies — message edits don't). Per-host status is shown live in the
// progress message above.
func (c *Command) notify(s *discordgo.Session, i *discordgo.InteractionCreate, network string, hosts int, runErr error) {
	mention := mentionUser(i)

	var content string
	if runErr != nil {
		content = fmt.Sprintf("%s ❌ Roll on **%s** aborted: %v", mention, network, runErr)
	} else {
		content = fmt.Sprintf("%s ✅ Roll on **%s** complete — %d host(s) updated and healthy.", mention, network, hosts)
	}

	if _, err := s.ChannelMessageSend(i.ChannelID, strings.TrimSpace(content)); err != nil {
		c.log.WithError(err).Debug("roll: failed to send completion notice")
	}
}

func mentionUser(i *discordgo.InteractionCreate) string {
	switch {
	case i.Member != nil && i.Member.User != nil:
		return "<@" + i.Member.User.ID + ">"
	case i.User != nil:
		return "<@" + i.User.ID + ">"
	default:
		return ""
	}
}

func (c *Command) actuator() (rollpkg.Actuator, error) {
	if c.cfg.Actuator == actuatorAPI {
		if c.cfg.WatchtowerToken == "" {
			return nil, fmt.Errorf("watchtower token not configured (WATCHTOWER_HTTP_API_TOKEN)")
		}

		return rollpkg.NewAPIActuator(c.cfg.WatchtowerToken, "https", 0, "watchtower-"), nil
	}

	return rollpkg.NewSSHActuator(rollpkg.SSHConfig{PrivateKeyPath: c.cfg.SSHKeyPath, Log: c.log})
}

// rollUI accumulates per-host progress into a renderable Discord message.
type rollUI struct {
	mu     sync.Mutex
	header string
	names  []string
	state  []string
	footer string
}

func newRollUI(network, client, image string, dryRun bool, targets []rollpkg.Target) *rollUI {
	mode := "Rolling"
	if dryRun {
		mode = "Dry-run"
	}

	header := fmt.Sprintf("**%s `%s`** on **%s**", mode, client, network)
	if image != "" {
		header += fmt.Sprintf(" — image `%s`", image)
	}

	header += fmt.Sprintf("  •  %d host(s)", len(targets))

	names := make([]string, len(targets))
	state := make([]string, len(targets))

	for idx, t := range targets {
		names[idx] = t.Name
		state[idx] = fmt.Sprintf("⏳ `%s`", t.Name)
	}

	return &rollUI{header: header, names: names, state: state}
}

func (u *rollUI) update(p rollpkg.Progress) {
	if p.Index < 1 || p.Index > len(u.state) {
		return // fleet-level event (e.g. Done) is handled by finish
	}

	u.mu.Lock()
	defer u.mu.Unlock()

	name := u.names[p.Index-1]

	switch p.Phase {
	case rollpkg.PhaseTriggering:
		u.state[p.Index-1] = fmt.Sprintf("🔄 `%s` — triggering…", name)
	case rollpkg.PhaseHealthy:
		u.state[p.Index-1] = fmt.Sprintf("✅ `%s`", name)
	case rollpkg.PhaseFailed:
		u.state[p.Index-1] = fmt.Sprintf("❌ `%s` — %s", name, p.Message)
	case rollpkg.PhaseSkipped:
		u.state[p.Index-1] = fmt.Sprintf("• `%s` — would roll", name)
	case rollpkg.PhaseDone:
	}
}

func (u *rollUI) finish(err error) {
	u.mu.Lock()
	defer u.mu.Unlock()

	if err != nil {
		u.footer = fmt.Sprintf("**Aborted:** %v\nRemaining hosts were left untouched.", err)

		return
	}

	u.footer = "**Done** ✅ — all targeted hosts rolled and healthy."
}

func (u *rollUI) render() string {
	u.mu.Lock()
	defer u.mu.Unlock()

	var b strings.Builder

	b.WriteString(u.header)
	b.WriteString("\n")

	for _, line := range u.state {
		b.WriteString(line)
		b.WriteString("\n")
	}

	if u.footer != "" {
		b.WriteString("\n")
		b.WriteString(u.footer)
	}

	return b.String()
}
