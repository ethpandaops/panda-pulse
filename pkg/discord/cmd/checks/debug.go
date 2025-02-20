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
)

func (c *ChecksCommand) handleDebug(s *discordgo.Session, i *discordgo.InteractionCreate, opt *discordgo.ApplicationCommandInteractionDataOption) error {
	// Acknowledge the interaction first.
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	}); err != nil {
		return fmt.Errorf("failed to acknowledge interaction: %w", err)
	}

	checkID := opt.Options[0].StringValue()

	// List all artifacts and find the one with matching ID.
	artifacts, err := c.bot.GetChecksRepo().List(context.Background())
	if err != nil {
		return fmt.Errorf("failed to list artifacts: %w", err)
	}

	var matchingArtifact *store.CheckArtifact
	for _, artifact := range artifacts {
		if artifact.CheckID == checkID {
			matchingArtifact = artifact

			break
		}
	}

	if matchingArtifact == nil {
		notFoundMsg := fmt.Sprintf("ℹ️ No check found with ID: %s", checkID)
		if _, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: &notFoundMsg,
		}); err != nil {
			return fmt.Errorf("failed to send not found message: %w", err)
		}

		return nil
	}

	// Get the log content.
	output, err := c.bot.GetChecksRepo().GetStore().GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String(c.bot.GetChecksRepo().GetBucket()),
		Key: aws.String(fmt.Sprintf("%s/networks/%s/checks/%s/%s.log",
			c.bot.GetChecksRepo().GetPrefix(),
			matchingArtifact.Network,
			matchingArtifact.Client,
			matchingArtifact.CheckID,
		)),
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

	// Create the embed.
	embed := &discordgo.MessageEmbed{
		Title: "Debug Log",
		Color: 0x7289DA,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "ID",
				Value:  fmt.Sprintf("`%s`", matchingArtifact.CheckID),
				Inline: false,
			},
			{
				Name:   "Network",
				Value:  fmt.Sprintf("🌐 `%s`", matchingArtifact.Network),
				Inline: true,
			},
			{
				Name:   "Client",
				Value:  fmt.Sprintf("`%s`", matchingArtifact.Client),
				Inline: true,
			},
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}

	// Add client logo if available.
	if logo := clients.GetClientLogo(matchingArtifact.Client); logo != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{
			URL: logo,
		}
	}

	// Send the embed first.
	_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{embed},
	})
	if err != nil {
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
