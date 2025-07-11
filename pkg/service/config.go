package service

import (
	"fmt"

	"github.com/ethpandaops/panda-pulse/pkg/cartographoor"
	"github.com/ethpandaops/panda-pulse/pkg/discord"
	"github.com/ethpandaops/panda-pulse/pkg/grafana"
	"github.com/ethpandaops/panda-pulse/pkg/hive"
	"github.com/ethpandaops/panda-pulse/pkg/store"
)

// Config contains the configuration for the service.
type Config struct {
	GrafanaToken       string
	DiscordToken       string
	GrafanaBaseURL     string
	PromDatasourceID   string
	AccessKeyID        string
	SecretAccessKey    string
	GithubToken        string
	S3Bucket           string
	S3BucketPrefix     string
	S3Region           string
	S3EndpointURL      string
	ClientsDataURL     string
	MetricsAddress     string // Defaults to :9091
	HealthCheckAddress string // Defaults to :9191
}

// AsS3Config converts the configuration to an S3Config.
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

// AsDiscordConfig converts the configuration to a DiscordConfig.
func (c *Config) AsDiscordConfig() *discord.Config {
	return &discord.Config{
		DiscordToken: c.DiscordToken,
		GithubToken:  c.GithubToken,
	}
}

// AsGrafanaConfig converts the configuration to a GrafanaConfig.
func (c *Config) AsGrafanaConfig() *grafana.Config {
	return &grafana.Config{
		Token:            c.GrafanaToken,
		PromDatasourceID: c.PromDatasourceID,
		BaseURL:          c.GrafanaBaseURL,
	}
}

// AsHiveConfig converts the configuration to a HiveConfig.
func (c *Config) AsHiveConfig() *hive.Config {
	return &hive.Config{
		BaseURL: hive.BaseURL,
	}
}

// AsCartographoorConfig converts the configuration to a CartographoorConfig.
func (c *Config) AsCartographoorConfig() cartographoor.ServiceConfig {
	return cartographoor.ServiceConfig{
		SourceURL: c.ClientsDataURL,
	}
}

// Validate validates the configuration.
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

	if c.GithubToken == "" {
		return fmt.Errorf("GITHUB_TOKEN environment variable is required")
	}

	return nil
}
