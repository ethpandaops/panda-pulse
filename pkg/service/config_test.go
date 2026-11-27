package service

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethpandaops/panda-pulse/pkg/githubauth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testPrivateKeyPEM(t *testing.T) string {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	return string(pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}))
}

func TestNewGithubTokenSource(t *testing.T) {
	keyPEM := testPrivateKeyPEM(t)

	keyFile := filepath.Join(t.TempDir(), "app.pem")
	require.NoError(t, os.WriteFile(keyFile, []byte(keyPEM), 0o600))

	tests := []struct {
		name    string
		cfg     Config
		wantPAT string
		wantApp bool
		wantErr string
	}{
		{
			name:    "token only",
			cfg:     Config{GithubToken: "pat"},
			wantPAT: "pat",
		},
		{
			name: "token takes precedence over app settings",
			cfg: Config{
				GithubToken:             "pat",
				GithubAppID:             "1",
				GithubAppInstallationID: "2",
				GithubAppPrivateKey:     keyPEM,
			},
			wantPAT: "pat",
		},
		{
			name: "app with inline key",
			cfg: Config{
				GithubAppID:             "1",
				GithubAppInstallationID: "2",
				GithubAppPrivateKey:     keyPEM,
			},
			wantApp: true,
		},
		{
			name: "app with key file",
			cfg: Config{
				GithubAppID:             "1",
				GithubAppInstallationID: "2",
				GithubAppPrivateKeyFile: keyFile,
			},
			wantApp: true,
		},
		{
			name:    "nothing configured",
			cfg:     Config{},
			wantErr: "either GITHUB_TOKEN or the GitHub App settings",
		},
		{
			name: "missing installation id",
			cfg: Config{
				GithubAppID:         "1",
				GithubAppPrivateKey: keyPEM,
			},
			wantErr: "GITHUB_APP_INSTALLATION_ID must be an integer",
		},
		{
			name: "non-numeric app id",
			cfg: Config{
				GithubAppID:             "abc",
				GithubAppInstallationID: "2",
				GithubAppPrivateKey:     keyPEM,
			},
			wantErr: "GITHUB_APP_ID must be an integer",
		},
		{
			name: "missing key",
			cfg: Config{
				GithubAppID:             "1",
				GithubAppInstallationID: "2",
			},
			wantErr: "exactly one of GITHUB_APP_PRIVATE_KEY or GITHUB_APP_PRIVATE_KEY_FILE",
		},
		{
			name: "both key and key file",
			cfg: Config{
				GithubAppID:             "1",
				GithubAppInstallationID: "2",
				GithubAppPrivateKey:     keyPEM,
				GithubAppPrivateKeyFile: keyFile,
			},
			wantErr: "exactly one of GITHUB_APP_PRIVATE_KEY or GITHUB_APP_PRIVATE_KEY_FILE",
		},
		{
			name: "invalid key",
			cfg: Config{
				GithubAppID:             "1",
				GithubAppInstallationID: "2",
				GithubAppPrivateKey:     "not a key",
			},
			wantErr: "failed to parse GitHub App private key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens, err := tt.cfg.NewGithubTokenSource(nil)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)

				return
			}

			require.NoError(t, err)

			if tt.wantApp {
				assert.IsType(t, &githubauth.AppTokenSource{}, tokens)

				return
			}

			assert.Equal(t, githubauth.StaticToken(tt.wantPAT), tokens)
		})
	}
}
