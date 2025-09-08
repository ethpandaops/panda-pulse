package discord

import (
	"context"
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/cartographoor"
	cmdchecks "github.com/ethpandaops/panda-pulse/pkg/discord/cmd/checks"
	"github.com/ethpandaops/panda-pulse/pkg/discord/cmd/common"
	cmdhive "github.com/ethpandaops/panda-pulse/pkg/discord/cmd/hive"
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
	GetCartographoor() *cartographoor.Service
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
	cartographoor   *cartographoor.Service
	commands        []common.Command
	metrics         *Metrics
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
	metrics *Metrics,
	cartographoor *cartographoor.Service,
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
		//clientsService:  clientsService,
		cartographoor: cartographoor,
		commands:      make([]common.Command, 0),
		metrics:       metrics,
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
			// Pass guild ID if available for guild-specific registration
			if registrar, ok := cmd.(interface {
				RegisterWithGuild(*discordgo.Session, string) error
			}); ok && b.config.GuildID != "" {
				if err := registrar.RegisterWithGuild(b.session, b.config.GuildID); err != nil {
					return fmt.Errorf("failed to register command with guild: %w", err)
				}
			} else {
				if err := cmd.Register(b.session); err != nil {
					return fmt.Errorf("failed to register command: %w", err)
				}
			}
		}
	}

	// If we have any existing monitor alerts configured, schedule them.
	if err := b.scheduleExistingAlerts(); err != nil {
		return fmt.Errorf("failed to schedule existing alerts: %w", err)
	}

	// Schedule periodic refresh of discord command choices.
	if err := b.scheduleDiscordChoiceRefresh(); err != nil {
		return fmt.Errorf("failed to schedule choice refresh: %w", err)
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

// GetCartographoor returns the cartographoor service.
func (b *DiscordBot) GetCartographoor() *cartographoor.Service {
	return b.cartographoor
}

// handleInteraction handles Discord command interactions.
func (b *DiscordBot) handleInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// Handle autocomplete interactions
	if i.Type == discordgo.InteractionApplicationCommandAutocomplete {
		data := i.ApplicationCommandData()
		for _, cmd := range b.commands {
			if cmd.Name() == data.Name {
				cmd.Handle(s, i)

				return
			}
		}

		return
	}

	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	data := i.ApplicationCommandData()
	for _, cmd := range b.commands {
		if cmd.Name() == data.Name {
			startTime := time.Now()

			// Get username
			username := "unknown"
			if i.Member != nil && i.Member.User != nil {
				username = i.Member.User.Username
			} else if i.User != nil {
				username = i.User.Username
			}

			// Get subcommand name
			var subcommand string
			if len(data.Options) > 0 {
				subcommand = data.Options[0].Name
			} else {
				subcommand = "none"
			}

			logCtx := logrus.Fields{
				"command":    cmd.Name(),
				"subcommand": subcommand,
				"guild":      i.GuildID,
				"user":       username,
				"roles":      common.GetRoleNames(i.Member, s, i.GuildID),
			}

			b.log.WithFields(logCtx).Info("Received command")

			// Record command execution
			b.metrics.RecordCommandExecution(cmd.Name(), subcommand, username)

			// Set last execution timestamp
			b.metrics.SetLastCommandTimestamp(cmd.Name(), subcommand, float64(time.Now().Unix()))

			// Skip permission check for /build trigger as it has its own permission handling
			if cmd.Name() == "build" && len(data.Options) > 0 && data.Options[0].Name == "trigger" {
				cmd.Handle(s, i)

				// Record command execution time
				executionTime := time.Since(startTime).Seconds()
				b.metrics.ObserveCommandDuration(cmd.Name(), subcommand, executionTime)

				return
			}

			// Check permissions before executing command.
			if !common.HasPermission(i.Member, s, i.GuildID, b.config.AsRoleConfig(), &data) {
				if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{
						Content: common.NoPermissionError(fmt.Sprintf("%s %s", cmd.Name(), subcommand)).Error(),
						Flags:   discordgo.MessageFlagsEphemeral,
					},
				}); err != nil {
					b.log.WithError(err).Error("Failed to respond with permission error")
				}

				// Record permission error
				b.metrics.RecordCommandError(cmd.Name(), subcommand, "permission_denied")

				b.log.WithFields(logCtx).Error("Permission denied")

				return
			}

			// Handle the command
			cmd.Handle(s, i)

			// Record command execution time
			executionTime := time.Since(startTime).Seconds()
			b.metrics.ObserveCommandDuration(cmd.Name(), subcommand, executionTime)

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
			"network":  alert.Network,
			"client":   alert.Client,
			"schedule": alert.Schedule,
		}).Info("Scheduling alert")

		// Use the alert's schedule if available, otherwise fall back to default
		schedule := cmdchecks.DefaultCheckSchedule
		if alert.Schedule != "" {
			schedule = alert.Schedule
		}

		if addErr := b.scheduler.AddJob(jobName, schedule, func(ctx context.Context) error {
			b.log.WithFields(logrus.Fields{
				"network": alert.Network,
				"client":  alert.Client,
			}).Info("Queueing alert")

			// Find the checks command.
			for _, cmd := range b.commands {
				if checksCmd, ok := cmd.(*cmdchecks.ChecksCommand); ok {
					checksCmd.Queue().Enqueue(alert)

					break
				}
			}

			return nil
		}); addErr != nil {
			return fmt.Errorf("failed to schedule alert: %w", addErr)
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

		jobName := fmt.Sprintf("hive-summary-%s", alert.Network)

		b.log.WithFields(logrus.Fields{
			"network":  alert.Network,
			"channel":  alert.DiscordChannel,
			"schedule": alert.Schedule,
		}).Info("Scheduling hive summary")

		if err := b.scheduler.AddJob(jobName, alert.Schedule, func(ctx context.Context) error {
			// Find the hive command.
			for _, cmd := range b.commands {
				if hiveCmd, ok := cmd.(*cmdhive.HiveCommand); ok {
					if err := hiveCmd.RunHiveSummary(ctx, alert); err != nil {
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

// RefreshCommandChoices refreshes the choices for all commands that support it.
func (b *DiscordBot) RefreshCommandChoices() error {
	b.log.Info("Refreshing command choices")

	var (
		successCount int
		failureCount int
		errors       []error
	)

	for _, cmd := range b.commands {
		// Check if command supports choice updates.
		if updater, ok := cmd.(interface {
			UpdateChoices(*discordgo.Session) error
		}); ok {
			if err := updater.UpdateChoices(b.session); err != nil {
				// Log the error but continue with other commands
				b.log.WithFields(logrus.Fields{
					"command": cmd.Name(),
					"error":   err,
				}).Error("Failed to update command choices")

				failureCount++

				errors = append(errors, fmt.Errorf("command %s: %w", cmd.Name(), err))
			} else {
				b.log.WithField("command", cmd.Name()).Info("Successfully updated command choices")

				successCount++
			}
		}
	}

	// Log summary
	b.log.WithFields(logrus.Fields{
		"success_count": successCount,
		"failure_count": failureCount,
	}).Info("Command choices refresh completed")

	// Return an error only if all updates failed
	if successCount == 0 && failureCount > 0 {
		return fmt.Errorf("all command choice updates failed: %v", errors)
	}

	return nil
}

// scheduleDiscordChoiceRefresh schedules periodic refresh of command choices. Our cartographoor service
// is updated every hour, so we need to refresh the command choices to reflect the latest data as once
// a discord command is registered, we need to refresh the choices to reflect any changes.
func (b *DiscordBot) scheduleDiscordChoiceRefresh() error {
	// Refresh choices every hour.
	if err := b.scheduler.AddJob("refresh-command-choices", "*/45 * * * *", func(ctx context.Context) error {
		b.log.Info("Running scheduled command choices refresh")

		return b.RefreshCommandChoices()
	}); err != nil {
		return fmt.Errorf("failed to schedule choice refresh: %w", err)
	}

	b.log.Info("Scheduled bot command refresh")

	return nil
}
