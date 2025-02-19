package discord

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/checks"
	"github.com/ethpandaops/panda-pulse/pkg/grafana"
	"github.com/ethpandaops/panda-pulse/pkg/scheduler"
	"github.com/ethpandaops/panda-pulse/pkg/store"
)

type Bot struct {
	config       *Config
	session      *discordgo.Session
	scheduler    *scheduler.Scheduler
	monitorRepo  *store.MonitorRepo
	checksRunner checks.Runner
	grafana      grafana.Client
	httpClient   *http.Client
	commands     map[string]Command
}

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

func NewBot(
	cfg *Config,
	scheduler *scheduler.Scheduler,
	monitorRepo *store.MonitorRepo,
	checksRunner checks.Runner,
	grafana grafana.Client,
) (*Bot, error) {
	session, err := discordgo.New("Bot " + cfg.DiscordToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create discord session: %w", err)
	}

	bot := &Bot{
		config:       cfg,
		session:      session,
		scheduler:    scheduler,
		monitorRepo:  monitorRepo,
		checksRunner: checksRunner,
		grafana:      grafana,
		httpClient:   &http.Client{},
		commands:     make(map[string]Command),
	}

	// Register command handlers
	checksCmd := NewChecksCommand(bot)
	bot.commands[checksCmd.Name()] = checksCmd

	session.AddHandler(bot.handleCommandInteraction)

	return bot, nil
}

func (b *Bot) Start() error {
	log.Printf("Opening Discord connection...")
	if err := b.session.Open(); err != nil {
		return fmt.Errorf("failed to open discord connection: %w", err)
	}

	log.Printf("Registering slash commands for bot ID: %s", b.session.State.User.ID)

	for _, cmd := range b.commands {
		log.Printf("Registering command: /%s", cmd.Name())
		if err := cmd.Register(b.session); err != nil {
			return fmt.Errorf("failed to register command %s: %w", cmd.Name(), err)
		}
	}

	log.Printf("Successfully registered slash commands")

	log.Printf("Loading existing alerts...")
	alerts, err := b.monitorRepo.ListMonitorAlerts(context.Background())
	if err != nil {
		return fmt.Errorf("failed to list alerts: %w", err)
	}
	log.Printf("Found %d existing alerts", len(alerts))

	cmd, ok := b.commands["checks"].(*ChecksCommand)
	if !ok {
		return fmt.Errorf("failed to get checks command")
	}

	for _, alert := range alerts {
		if err := cmd.ScheduleAlert(alert); err != nil {
			return fmt.Errorf("failed to schedule existing alert for %s: %w", alert.Network, err)
		}
	}

	return nil
}

func (b *Bot) Stop() error {
	return b.session.Close()
}

// handleCommandInteraction routes incoming Discord interactions to the appropriate command handler.
func (b *Bot) handleCommandInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	data := i.ApplicationCommandData()
	if cmd, ok := b.commands[data.Name]; ok {
		cmd.Handle(s, i)
	}
}
