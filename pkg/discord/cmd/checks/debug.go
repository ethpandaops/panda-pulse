package checks

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/clients"
	"github.com/ethpandaops/panda-pulse/pkg/store"
	"github.com/sirupsen/logrus"
)

const (
	msgNoCheckFound = "ℹ️ No check found with ID: %s"
	debugEmbedColor = 0x7289DA
)

func (c *ChecksCommand) handleDebug(
	s *discordgo.Session,
	i *discordgo.InteractionCreate,
	opt *discordgo.ApplicationCommandInteractionDataOption,
) error {
	// Acknowledge the interaction first.
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	}); err != nil {
		return fmt.Errorf("failed to acknowledge interaction: %w", err)
	}

	var (
		checkID = opt.Options[0].StringValue()
		guildID = i.GuildID
	)

	c.log.WithFields(logrus.Fields{
		"command": "/checks debug",
		"checkID": checkID,
		"guild":   guildID,
		"user":    i.Member.User.Username,
	}).Info("Received command")

	// List all artifacts and find the one with matching ID.
	artifacts, err := c.bot.GetChecksRepo().List(context.Background())
	if err != nil {
		return fmt.Errorf("failed to list artifacts: %w", err)
	}

	var matchingArtifact *store.CheckArtifact

	// Get all alerts to check if the check ID belongs to this guild
	alerts, err := c.bot.GetMonitorRepo().List(context.Background())
	if err != nil {
		return fmt.Errorf("failed to list alerts: %w", err)
	}

	// Create a map of check IDs that belong to this guild
	guildCheckIDs := make(map[string]bool)

	for _, alert := range alerts {
		if alert.DiscordGuildID == guildID && alert.CheckID != "" {
			guildCheckIDs[alert.CheckID] = true
		}
	}

	for _, artifact := range artifacts {
		if artifact.CheckID == checkID {
			// Only show artifacts for checks that belong to this guild
			if guildCheckIDs[checkID] {
				matchingArtifact = artifact

				break
			}
		}
	}

	if matchingArtifact == nil {
		if _, ierr := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: stringPtr(fmt.Sprintf(msgNoCheckFound, checkID)),
		}); ierr != nil {
			return fmt.Errorf("failed to send not found message: %w", ierr)
		}

		return nil
	}

	// Get the log content.
	output, err := c.bot.GetChecksRepo().GetStore().GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String(c.bot.GetChecksRepo().GetBucket()),
		Key:    aws.String(c.getLogPath(matchingArtifact)),
	})
	if err != nil {
		return fmt.Errorf("failed to get log content: %w", err)
	}

	defer output.Body.Close()

	// Read the log content.
	logContent, err := io.ReadAll(output.Body)
	if err != nil {
		return fmt.Errorf("failed to read log content: %w", err)
	}

	// Send the embed first.
	embed := buildDebugEmbed(matchingArtifact)
	if _, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{embed},
	}); err != nil {
		return fmt.Errorf("failed to send embed: %w", err)
	}

	// Follow up with the log file.
	if _, err = s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Files: []*discordgo.File{
			{
				Name:        fmt.Sprintf("%s.log", matchingArtifact.CheckID),
				ContentType: "text/plain",
				Reader:      bytes.NewReader(logContent),
			},
		},
	}); err != nil {
		return fmt.Errorf("failed to send log file: %w", err)
	}

	return nil
}

// buildDebugEmbed creates the debug log embed.
func buildDebugEmbed(artifact *store.CheckArtifact) *discordgo.MessageEmbed {
	embed := &discordgo.MessageEmbed{
		Title: "Debug Log",
		Color: debugEmbedColor,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "ID",
				Value:  fmt.Sprintf("`%s`", artifact.CheckID),
				Inline: false,
			},
			{
				Name:   "Network",
				Value:  fmt.Sprintf("🌐 `%s`", artifact.Network),
				Inline: true,
			},
			{
				Name:   "Client",
				Value:  fmt.Sprintf("`%s`", artifact.Client),
				Inline: true,
			},
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}

	if logo := clients.GetClientLogo(artifact.Client); logo != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{
			URL: logo,
		}
	}

	return embed
}

// getLogPath returns the S3 path for a check's log file.
func (c *ChecksCommand) getLogPath(artifact *store.CheckArtifact) string {
	return fmt.Sprintf(
		"%s/networks/%s/checks/%s/%s.log",
		c.bot.GetChecksRepo().GetPrefix(),
		artifact.Network,
		artifact.Client,
		artifact.CheckID,
	)
}
