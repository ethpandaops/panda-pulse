package service

import (
	"fmt"

	"github.com/ethpandaops/panda-pulse/pkg/discord"
	"github.com/ethpandaops/panda-pulse/pkg/store"
)

type Config struct {
	Network          string
	ConsensusNode    string
	ExecutionNode    string
	DiscordChannel   string
	GrafanaToken     string
	DiscordToken     string
	GrafanaBaseURL   string
	PromDatasourceID string
	AccessKeyID      string
	SecretAccessKey  string
	S3Bucket         string
	S3BucketPrefix   string
	S3Region         string
	S3EndpointURL    string
}

func (c *Config) Validate() error {
	if c.GrafanaToken == "" {
		return fmt.Errorf("GRAFANA_SERVICE_TOKEN environment variable is required")
	}

	if c.DiscordToken == "" {
		return fmt.Errorf("DISCORD_BOT_TOKEN environment variable is required")
	}

	if c.AccessKeyID == "" {
		return fmt.Errorf("AWS_ACCESS_KEY_ID environment variable is required")
	}

	if c.SecretAccessKey == "" {
		return fmt.Errorf("AWS_SECRET_ACCESS_KEY environment variable is required")
	}

	if c.S3Bucket == "" {
		return fmt.Errorf("S3_BUCKET environment variable is required")
	}

	return nil
}

func (c *Config) AsS3Config() *store.S3Config {
	return &store.S3Config{
		AccessKeyID:     c.AccessKeyID,
		SecretAccessKey: c.SecretAccessKey,
		Bucket:          c.S3Bucket,
		Prefix:          c.S3BucketPrefix,
		Region:          c.S3Region,
		EndpointURL:     c.S3EndpointURL,
	}
}

func (c *Config) AsDiscordConfig() *discord.Config {
	return &discord.Config{
		GrafanaToken: c.GrafanaToken,
		DiscordToken: c.DiscordToken,
	}
}
