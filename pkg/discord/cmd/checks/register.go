package checks

import (
	"context"
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/clients"
	"github.com/ethpandaops/panda-pulse/pkg/store"
	"github.com/sirupsen/logrus"
)

const (
	msgAlreadyRegistered = "ℹ️ Client **%s** is already registered for **%s** in <#%s>"
	msgRegisteredClient  = "✅ Successfully registered **%s** for **%s** notifications in <#%s>"
	msgRegisteredAll     = "✅ Successfully registered **all clients** for **%s** notifications in <#%s>"
	defaultSchedule      = "*/1 * * * *"
)

// handleRegister handles the '/checks register' command.
func (c *ChecksCommand) handleRegister(
	s *discordgo.Session,
	i *discordgo.InteractionCreate,
	data *discordgo.ApplicationCommandInteractionDataOption,
) error {
	var (
		options = data.Options
		network = options[0].StringValue()
		channel = options[1].ChannelValue(s)
		client  *string
	)

	if len(options) > 2 {
		c := options[2].StringValue()
		client = &c
	}

	c.log.WithFields(logrus.Fields{
		"command": "/checks register",
		"network": network,
		"channel": channel.Name,
		"user":    i.Member.User.Username,
	}).Info("Received command")

	if err := c.registerAlert(context.Background(), network, channel.ID, client); err != nil {
		if alreadyRegistered, ok := err.(*store.AlertAlreadyRegisteredError); ok {
			return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: fmt.Sprintf(msgAlreadyRegistered, alreadyRegistered.Client, network, channel.ID),
				},
			})
		}

		return fmt.Errorf("failed to register alert: %w", err)
	}

	var msg string

	if client != nil {
		msg = fmt.Sprintf(msgRegisteredClient, *client, network, channel.ID)
	} else {
		msg = fmt.Sprintf(msgRegisteredAll, network, channel.ID)
	}

	return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: msg,
		},
	})
}

func (c *ChecksCommand) registerAlert(ctx context.Context, network, channelID string, specificClient *string) error {
	if specificClient == nil {
		return c.registerAllClients(ctx, network, channelID)
	}

	// Check if this specific client is already registered.
	alerts, err := c.bot.GetMonitorRepo().List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list alerts: %w", err)
	}

	for _, alert := range alerts {
		if alert.Network == network && alert.Client == *specificClient && alert.DiscordChannel == channelID {
			return &store.AlertAlreadyRegisteredError{
				Network: network,
				Channel: channelID,
				Client:  *specificClient,
			}
		}
	}

	clientType := getClientType(*specificClient)
	if clientType == clients.ClientTypeAll {
		return fmt.Errorf("unknown client: %s", *specificClient)
	}

	alert := newMonitorAlert(network, *specificClient, clientType, channelID)
	if err := c.scheduleAlert(ctx, alert); err != nil {
		return fmt.Errorf("failed to schedule alert: %w", err)
	}

	return nil
}

// registerAllClients registers a monitor alert for all clients for a given network.
func (c *ChecksCommand) registerAllClients(ctx context.Context, network, channelID string) error {
	// Register CL clients.
	for _, client := range clients.CLClients {
		alert := newMonitorAlert(network, client, clients.ClientTypeCL, channelID)
		if err := c.scheduleAlert(ctx, alert); err != nil {
			return fmt.Errorf("failed to schedule CL alert: %w", err)
		}
	}

	// Register EL clients.
	for _, client := range clients.ELClients {
		alert := newMonitorAlert(network, client, clients.ClientTypeEL, channelID)
		if err := c.scheduleAlert(ctx, alert); err != nil {
			return fmt.Errorf("failed to schedule EL alert: %w", err)
		}
	}

	return nil
}

// scheduleAlert schedules a monitor alert to run every minute.
func (c *ChecksCommand) scheduleAlert(ctx context.Context, alert *store.MonitorAlert) error {
	// Firstly, persist the alert to our store.
	if err := c.bot.GetMonitorRepo().Persist(ctx, alert); err != nil {
		return err
	}

	jobName := c.bot.GetMonitorRepo().Key(alert)

	c.log.WithFields(logrus.Fields{
		"network": alert.Network,
		"channel": alert.DiscordChannel,
		"client":  alert.Client,
		"key":     jobName,
	}).Info("Registered monitor")

	// And secondly, schedule the alert to run on our schedule.
	if err := c.bot.GetScheduler().AddJob(jobName, defaultSchedule, func(ctx context.Context) error {
		c.log.WithFields(logrus.Fields{
			"network": alert.Network,
			"client":  alert.Client,
			"key":     jobName,
		}).Info("Running checks")

		_, err := c.RunChecks(ctx, alert)

		return err
	}); err != nil {
		return fmt.Errorf("failed to schedule alert: %w", err)
	}

	c.log.WithFields(logrus.Fields{
		"schedule": defaultSchedule,
		"key":      jobName,
	}).Info("Scheduled monitor alert")

	return nil
}

// newMonitorAlert creates a new monitor alert with the given parameters.
func newMonitorAlert(network, client string, clientType clients.ClientType, channelID string) *store.MonitorAlert {
	now := time.Now()

	return &store.MonitorAlert{
		Network:        network,
		Client:         client,
		ClientType:     clientType,
		DiscordChannel: channelID,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

// getClientType determines the client type from a client name.
func getClientType(clientName string) clients.ClientType {
	for _, c := range clients.CLClients {
		if c == clientName {
			return clients.ClientTypeCL
		}
	}

	for _, c := range clients.ELClients {
		if c == clientName {
			return clients.ClientTypeEL
		}
	}

	return clients.ClientTypeAll
}
