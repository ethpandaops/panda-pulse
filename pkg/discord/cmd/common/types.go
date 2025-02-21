package common

import (
	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/grafana"
	"github.com/ethpandaops/panda-pulse/pkg/hive"
	"github.com/ethpandaops/panda-pulse/pkg/scheduler"
	"github.com/ethpandaops/panda-pulse/pkg/store"
)

// Command represents a Discord slash command.
type Command interface {
	// Name returns the name of the command.
	Name() string
	// Register registers the command with the given session.
	Register(*discordgo.Session) error
	// Handle handles the command.
	Handle(*discordgo.Session, *discordgo.InteractionCreate)
}

// BotContext provides access to bot functionality needed by commands.
type BotContext interface {
	// GetSession returns the Discord session.
	GetSession() *discordgo.Session
	// GetScheduler returns the scheduler.
	GetScheduler() *scheduler.Scheduler
	// GetMonitorRepo returns the monitor repository.
	GetMonitorRepo() *store.MonitorRepo
	// GetChecksRepo returns the checks repository.
	GetChecksRepo() *store.ChecksRepo
	// GetMentionsRepo returns the mentions repository.
	GetMentionsRepo() *store.MentionsRepo
	// GetGrafana returns the Grafana client.
	GetGrafana() grafana.Client
	// GetHive returns the Hive client.
	GetHive() hive.Hive
}
