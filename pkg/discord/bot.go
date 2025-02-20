package discord

import (
	"context"
	"fmt"

	"github.com/bwmarrin/discordgo"
	cmdchecks "github.com/ethpandaops/panda-pulse/pkg/discord/cmd/checks"
	"github.com/ethpandaops/panda-pulse/pkg/discord/cmd/common"
	"github.com/ethpandaops/panda-pulse/pkg/grafana"
	"github.com/ethpandaops/panda-pulse/pkg/hive"
	"github.com/ethpandaops/panda-pulse/pkg/scheduler"
	"github.com/ethpandaops/panda-pulse/pkg/store"
	"github.com/sirupsen/logrus"
)

// Bot represents the Discord bot.
type Bot struct {
	log         *logrus.Logger
	config      *Config
	session     *discordgo.Session
	scheduler   *scheduler.Scheduler
	monitorRepo *store.MonitorRepo
	checksRepo  *store.ChecksRepo
	grafana     grafana.Client
	hive        hive.Hive
	commands    []common.Command
}

// NewBot creates a new Discord bot.
func NewBot(
	log *logrus.Logger,
	cfg *Config,
	scheduler *scheduler.Scheduler,
	monitorRepo *store.MonitorRepo,
	checksRepo *store.ChecksRepo,
	grafana grafana.Client,
	hive hive.Hive,
) (*Bot, error) {
	// Create a new Discord session.
	session, err := discordgo.New("Bot " + cfg.DiscordToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create discord session: %w", err)
	}

	bot := &Bot{
		log:         log,
		config:      cfg,
		session:     session,
		scheduler:   scheduler,
		monitorRepo: monitorRepo,
		checksRepo:  checksRepo,
		grafana:     grafana,
		hive:        hive,
		commands:    make([]common.Command, 0),
	}

	// Register command handlers.
	bot.commands = append(bot.commands, cmdchecks.NewChecksCommand(log, bot))

	// Register event handlers.
	session.AddHandler(bot.handleInteraction)

	return bot, nil
}

// Start starts the bot.
func (b *Bot) Start() error {
	// Open connection with Discord.
	if err := b.session.Open(); err != nil {
		return fmt.Errorf("failed to open discord connection: %w", err)
	}

	// Register application commands.
	for _, cmd := range b.commands {
		if err := cmd.Register(b.session); err != nil {
			return fmt.Errorf("failed to register command: %w", err)
		}
	}

	// If we have any existing monitor alerts configured, schedule them.
	if err := b.scheduleExistingAlerts(); err != nil {
		return fmt.Errorf("failed to schedule existing alerts: %w", err)
	}

	return nil
}

// Stop stops the bot.
func (b *Bot) Stop() error {
	return b.session.Close()
}

// GetSession returns the Discord session.
func (b *Bot) GetSession() *discordgo.Session {
	return b.session
}

// GetScheduler returns the scheduler.
func (b *Bot) GetScheduler() *scheduler.Scheduler {
	return b.scheduler
}

// GetMonitorRepo returns the monitor repository.
func (b *Bot) GetMonitorRepo() *store.MonitorRepo {
	return b.monitorRepo
}

// GetChecksRepo returns the checks repository.
func (b *Bot) GetChecksRepo() *store.ChecksRepo {
	return b.checksRepo
}

// GetGrafana returns the Grafana client.
func (b *Bot) GetGrafana() grafana.Client {
	return b.grafana
}

// GetHive returns the Hive client.
func (b *Bot) GetHive() hive.Hive {
	return b.hive
}

// handleInteraction handles interactions from the Discord client.
func (b *Bot) handleInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	data := i.ApplicationCommandData()
	for _, cmd := range b.commands {
		if cmd.Name() == data.Name {
			cmd.Handle(s, i)

			return
		}
	}
}

// scheduleExistingAlerts schedules existing monitor alerts.
func (b *Bot) scheduleExistingAlerts() error {
	alerts, err := b.monitorRepo.List(context.Background())
	if err != nil {
		return fmt.Errorf("failed to list alerts: %w", err)
	}

	b.log.WithFields(logrus.Fields{
		"count": len(alerts),
	}).Info("Existing monitor alerts requiring scheduling")

	if len(alerts) == 0 {
		return nil
	}

	checksCmd := b.getChecksCmd()
	if checksCmd == nil {
		return fmt.Errorf("checks command not found")
	}

	for _, alert := range alerts {
		schedule := "*/1 * * * *"
		jobName := b.monitorRepo.Key(alert)

		// Create a copy of the alert for the closure.
		alertCopy := alert

		// Add it to the scheduler.
		if err := b.scheduler.AddJob(jobName, schedule, func(ctx context.Context) error {
			_, err := checksCmd.RunChecks(ctx, alertCopy)

			return err
		}); err != nil {
			return fmt.Errorf("failed to schedule alert for %s: %w", alert.Network, err)
		}

		b.log.WithFields(logrus.Fields{
			"network":  alert.Network,
			"channel":  alert.DiscordChannel,
			"client":   alert.Client,
			"schedule": schedule,
			"key":      jobName,
		}).Info("Scheduled monitor alert")
	}

	return nil
}

// getChecksCmd returns the checks command.
func (b *Bot) getChecksCmd() *cmdchecks.ChecksCommand {
	for _, cmd := range b.commands {
		if c, ok := cmd.(*cmdchecks.ChecksCommand); ok {
			return c
		}
	}

	return nil
}
