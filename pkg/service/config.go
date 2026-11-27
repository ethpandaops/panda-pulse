package service

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/ethpandaops/panda-pulse/pkg/cartographoor"
	"github.com/ethpandaops/panda-pulse/pkg/discord"
	"github.com/ethpandaops/panda-pulse/pkg/githubauth"
	"github.com/ethpandaops/panda-pulse/pkg/grafana"
	"github.com/ethpandaops/panda-pulse/pkg/hive"
	"github.com/ethpandaops/panda-pulse/pkg/store"
)

// Config contains the configuration for the service.
type Config struct {
	GrafanaToken       string
	DiscordToken       string
	DiscordGuildIDs    []string // Optional: if set, commands will be registered to these guilds only
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

	// GitHub App credentials, used when GithubToken is empty.
	GithubAppID             string
	GithubAppInstallationID string
	GithubAppPrivateKey     string // PEM-encoded private key.
	GithubAppPrivateKeyFile string // Path to a PEM-encoded private key.
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
		GuildIDs:     c.DiscordGuildIDs,
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

	return c.validateGithubAuth()
}

// usesGithubApp reports whether GitHub API calls authenticate as a GitHub
// App. GITHUB_TOKEN takes precedence when both are configured.
func (c *Config) usesGithubApp() bool {
	return c.GithubToken == ""
}

// validateGithubAuth checks that either GITHUB_TOKEN or a complete set of
// GitHub App settings is configured.
func (c *Config) validateGithubAuth() error {
	if !c.usesGithubApp() {
		return nil
	}

	if c.GithubAppID == "" && c.GithubAppInstallationID == "" &&
		c.GithubAppPrivateKey == "" && c.GithubAppPrivateKeyFile == "" {
		return errors.New("either GITHUB_TOKEN or the GitHub App settings (GITHUB_APP_ID, " +
			"GITHUB_APP_INSTALLATION_ID and GITHUB_APP_PRIVATE_KEY or GITHUB_APP_PRIVATE_KEY_FILE) are required")
	}

	if _, err := strconv.ParseInt(c.GithubAppID, 10, 64); err != nil {
		return fmt.Errorf("GITHUB_APP_ID must be an integer: %w", err)
	}

	if _, err := strconv.ParseInt(c.GithubAppInstallationID, 10, 64); err != nil {
		return fmt.Errorf("GITHUB_APP_INSTALLATION_ID must be an integer: %w", err)
	}

	if (c.GithubAppPrivateKey == "") == (c.GithubAppPrivateKeyFile == "") {
		return errors.New("exactly one of GITHUB_APP_PRIVATE_KEY or GITHUB_APP_PRIVATE_KEY_FILE is required")
	}

	return nil
}

// NewGithubTokenSource returns the credentials for GitHub API calls:
// GITHUB_TOKEN when set, otherwise GitHub App installation tokens.
func (c *Config) NewGithubTokenSource(httpClient *http.Client) (githubauth.TokenSource, error) {
	if err := c.validateGithubAuth(); err != nil {
		return nil, err
	}

	if !c.usesGithubApp() {
		return githubauth.StaticToken(c.GithubToken), nil
	}

	appID, _ := strconv.ParseInt(c.GithubAppID, 10, 64)
	installationID, _ := strconv.ParseInt(c.GithubAppInstallationID, 10, 64)

	key := []byte(c.GithubAppPrivateKey)

	if c.GithubAppPrivateKeyFile != "" {
		var err error

		key, err = os.ReadFile(c.GithubAppPrivateKeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read GITHUB_APP_PRIVATE_KEY_FILE: %w", err)
		}
	}

	return githubauth.NewAppTokenSource(githubauth.AppConfig{
		AppID:          appID,
		InstallationID: installationID,
		PrivateKey:     key,
	}, httpClient)
}
