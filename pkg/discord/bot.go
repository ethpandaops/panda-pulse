package discord

import (
	"context"
	"fmt"

	"github.com/bwmarrin/discordgo"
	cmdchecks "github.com/ethpandaops/panda-pulse/pkg/discord/cmd/checks"
	"github.com/ethpandaops/panda-pulse/pkg/discord/cmd/common"
	"github.com/ethpandaops/panda-pulse/pkg/grafana"
	"github.com/ethpandaops/panda-pulse/pkg/hive"
	"github.com/ethpandaops/panda-pulse/pkg/queue"
	"github.com/ethpandaops/panda-pulse/pkg/scheduler"
	"github.com/ethpandaops/panda-pulse/pkg/store"
	"github.com/sirupsen/logrus"
)

//go:generate mockgen -package mock -destination mock/bot.mock.go github.com/ethpandaops/panda-pulse/pkg/discord Bot

// BotCore is the core interface for the Discord bot.
type BotCore interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	GetSession() *discordgo.Session
}

// BotServices is the services interface for the Discord bot.
type BotServices interface {
	GetScheduler() *scheduler.Scheduler
	GetMonitorRepo() *store.MonitorRepo
	GetChecksRepo() *store.ChecksRepo
	GetMentionsRepo() *store.MentionsRepo
	GetHiveSummaryRepo() *store.HiveSummaryRepo
	GetGrafana() grafana.Client
	GetHive() hive.Hive
}

// Bot is the interface for the Discord bot.
type Bot interface {
	BotCore
	BotServices
	GetRoleConfig() *common.RoleConfig
	SetCommands(commands []common.Command)
	GetQueues() []queue.Queuer
}

// DiscordBot represents the Discord bot implementation.
type DiscordBot struct {
	log             *logrus.Logger
	config          *Config
	session         *discordgo.Session
	scheduler       *scheduler.Scheduler
	monitorRepo     *store.MonitorRepo
	checksRepo      *store.ChecksRepo
	mentionsRepo    *store.MentionsRepo
	hiveSummaryRepo *store.HiveSummaryRepo
	grafana         grafana.Client
	hive            hive.Hive
	commands        []common.Command
}

// NewBot creates a new Discord bot.
func NewBot(
	log *logrus.Logger,
	cfg *Config,
	scheduler *scheduler.Scheduler,
	monitorRepo *store.MonitorRepo,
	checksRepo *store.ChecksRepo,
	mentionsRepo *store.MentionsRepo,
	hiveSummaryRepo *store.HiveSummaryRepo,
	grafana grafana.Client,
	hive hive.Hive,
) (Bot, error) {
	// Create a new Discord session.
	session, err := discordgo.New("Bot " + cfg.DiscordToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create discord session: %w", err)
	}

	bot := &DiscordBot{
		log:             log,
		config:          cfg,
		session:         session,
		scheduler:       scheduler,
		monitorRepo:     monitorRepo,
		checksRepo:      checksRepo,
		mentionsRepo:    mentionsRepo,
		hiveSummaryRepo: hiveSummaryRepo,
		grafana:         grafana,
		hive:            hive,
		commands:        make([]common.Command, 0),
	}

	// Register event handlers.
	session.AddHandler(bot.handleInteraction)

	return bot, nil
}

// SetCommands sets the commands for the bot.
func (b *DiscordBot) SetCommands(commands []common.Command) {
	b.commands = commands
}

// Start starts the bot.
func (b *DiscordBot) Start(ctx context.Context) error {
	// Open connection with Discord.
	if err := b.session.Open(); err != nil {
		return fmt.Errorf("failed to open discord connection: %w", err)
	}

	for _, cmd := range b.commands {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if err := cmd.Register(b.session); err != nil {
				return fmt.Errorf("failed to register command: %w", err)
			}
		}
	}

	// If we have any existing monitor alerts configured, schedule them.
	if err := b.scheduleExistingAlerts(); err != nil {
		return fmt.Errorf("failed to schedule existing alerts: %w", err)
	}

	return nil
}

// Stop stops the bot.
func (b *DiscordBot) Stop(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return b.session.Close()
	}
}

// GetSession returns the Discord session.
func (b *DiscordBot) GetSession() *discordgo.Session {
	return b.session
}

// GetScheduler returns the scheduler.
func (b *DiscordBot) GetScheduler() *scheduler.Scheduler {
	return b.scheduler
}

// GetMonitorRepo returns the monitor repository.
func (b *DiscordBot) GetMonitorRepo() *store.MonitorRepo {
	return b.monitorRepo
}

// GetChecksRepo returns the checks repository.
func (b *DiscordBot) GetChecksRepo() *store.ChecksRepo {
	return b.checksRepo
}

// GetMentionsRepo returns the mentions repository.
func (b *DiscordBot) GetMentionsRepo() *store.MentionsRepo {
	return b.mentionsRepo
}

// GetHiveSummaryRepo returns the Hive summary repository.
func (b *DiscordBot) GetHiveSummaryRepo() *store.HiveSummaryRepo {
	return b.hiveSummaryRepo
}

// GetGrafana returns the Grafana client.
func (b *DiscordBot) GetGrafana() grafana.Client {
	return b.grafana
}

// GetHive returns the Hive client.
func (b *DiscordBot) GetHive() hive.Hive {
	return b.hive
}

// handleInteraction handles interactions from the Discord client.
func (b *DiscordBot) handleInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	data := i.ApplicationCommandData()
	for _, cmd := range b.commands {
		if cmd.Name() == data.Name {
			// Check permissions before executing command.
			if !common.HasPermission(i.Member, s, i.GuildID, b.config.AsRoleConfig(), &data) {
				if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{
						Content: common.NoPermissionError(fmt.Sprintf("%s %s", cmd.Name(), data.Options[0].Name)).Error(),
					},
				}); err != nil {
					b.log.WithError(err).Error("Failed to respond with permission error")
				}

				return
			}

			cmd.Handle(s, i)

			return
		}
	}
}

// scheduleExistingAlerts schedules all existing alerts.
func (b *DiscordBot) scheduleExistingAlerts() error {
	ctx := context.Background()

	// Schedule monitor alerts.
	alerts, err := b.monitorRepo.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list alerts: %w", err)
	}

	for _, alert := range alerts {
		if !alert.Enabled {
			continue
		}

		jobName := b.monitorRepo.Key(alert)

		b.log.WithFields(logrus.Fields{
			"network": alert.Network,
			"client":  alert.Client,
			"key":     jobName,
		}).Info("Scheduling existing monitor alert")

		if err := b.scheduler.AddJob(jobName, cmdchecks.DefaultCheckSchedule, func(ctx context.Context) error {
			b.log.WithFields(logrus.Fields{
				"network": alert.Network,
				"client":  alert.Client,
				"key":     jobName,
			}).Info("Queueing registered check")

			// Find the checks command.
			for _, cmd := range b.commands {
				if checksCmd, ok := cmd.(*cmdchecks.ChecksCommand); ok {
					checksCmd.Queue().Enqueue(alert)
					break
				}
			}

			return nil
		}); err != nil {
			return fmt.Errorf("failed to schedule alert: %w", err)
		}
	}

	// Schedule Hive summary alerts.
	hiveAlerts, err := b.hiveSummaryRepo.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list Hive summary alerts: %w", err)
	}

	for _, alert := range hiveAlerts {
		if !alert.Enabled {
			continue
		}

		jobName := fmt.Sprintf("hive_summary_%s", alert.Network)

		b.log.WithFields(logrus.Fields{
			"network": alert.Network,
			"channel": alert.DiscordChannel,
			"key":     jobName,
		}).Info("Scheduling existing Hive summary alert")

		if err := b.scheduler.AddJob(jobName, alert.Schedule, func(ctx context.Context) error {
			b.log.WithFields(logrus.Fields{
				"network": alert.Network,
				"key":     jobName,
			}).Info("Running Hive summary check")

			// Find the checks command.
			for _, cmd := range b.commands {
				if checksCmd, ok := cmd.(*cmdchecks.ChecksCommand); ok {
					if err := checksCmd.RunHiveSummary(ctx, alert); err != nil {
						b.log.WithError(err).Error("Failed to run Hive summary check")
					}
					break
				}
			}

			return nil
		}); err != nil {
			return fmt.Errorf("failed to schedule Hive summary alert: %w", err)
		}
	}

	return nil
}

// GetChecksCmd returns the checks command.
func (b *DiscordBot) GetChecksCmd() *cmdchecks.ChecksCommand {
	for _, cmd := range b.commands {
		if c, ok := cmd.(*cmdchecks.ChecksCommand); ok {
			return c
		}
	}

	return nil
}

// GetRoleConfig returns the role configuration.
func (b *DiscordBot) GetRoleConfig() *common.RoleConfig {
	return b.config.AsRoleConfig()
}

// GetQueues returns all queues managed by the bot.
func (b *DiscordBot) GetQueues() []queue.Queuer {
	var queues []queue.Queuer

	// Add checks queue if available
	if checksCmd := b.GetChecksCmd(); checksCmd != nil {
		if q := checksCmd.Queue(); q != nil {
			queues = append(queues, q)
		}
	}

	return queues
}
