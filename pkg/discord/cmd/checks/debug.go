package checks

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/store"
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
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "🔍 Debugging check...",
			Flags:   discordgo.MessageFlagsEphemeral,
		},
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

	// Send the response.
	if _, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: stringPtr(fmt.Sprintf("✅ Debug logs found for **`%s`**", matchingArtifact.CheckID)),
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
		Flags: discordgo.MessageFlagsEphemeral,
	}); err != nil {
		return fmt.Errorf("failed to send log file: %w", err)
	}

	return nil
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
