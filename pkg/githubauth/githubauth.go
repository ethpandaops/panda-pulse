// Package githubauth provides the credentials used to call the GitHub API,
// either a static personal access token or short-lived GitHub App
// installation access tokens scoped to a single repository.
package githubauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Permissions maps a GitHub App permission name (e.g. "actions") to an
// access level ("read" or "write").
type Permissions map[string]string

// Permission sets used by the bot's GitHub API calls.
var (
	ActionsRead      = Permissions{"actions": "read"}
	ActionsWrite     = Permissions{"actions": "write"}
	ContentsRead     = Permissions{"contents": "read"}
	MetadataRead     = Permissions{"metadata": "read"}
	PullRequestsRead = Permissions{"pull_requests": "read"}
)

// ErrNotInstalled is returned when the GitHub App is not installed on the
// requested repository.
var ErrNotInstalled = errors.New("GitHub App is not installed on")

// TokenSource returns tokens for GitHub API requests.
type TokenSource interface {
	// Token returns a token that can call the GitHub API on repository
	// ("owner/name") with the given permissions.
	Token(ctx context.Context, repository string, perms Permissions) (string, error)

	// ReadToken returns a token for reading a public repository. An empty
	// token means the request should be sent unauthenticated.
	ReadToken(ctx context.Context, repository string, perms Permissions) (string, error)
}

// StaticToken is a TokenSource that returns the same token for every request.
type StaticToken string

// Token returns the static token.
func (t StaticToken) Token(_ context.Context, _ string, _ Permissions) (string, error) {
	return string(t), nil
}

// ReadToken returns the static token.
func (t StaticToken) ReadToken(_ context.Context, _ string, _ Permissions) (string, error) {
	return string(t), nil
}

// SetAuthorization sets the Authorization header of req to a token from
// tokens for repository and perms.
func SetAuthorization(ctx context.Context, req *http.Request, tokens TokenSource, repository string, perms Permissions) error {
	token, err := tokens.Token(ctx, repository, perms)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+token)

	return nil
}

// SetReadAuthorization sets the Authorization header of req for a read of a
// public repository, and leaves it unset when tokens has no token for
// repository.
func SetReadAuthorization(ctx context.Context, req *http.Request, tokens TokenSource, repository string, perms Permissions) error {
	token, err := tokens.ReadToken(ctx, repository, perms)
	if err != nil {
		return err
	}

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	return nil
}

// splitRepository splits "owner/name" into its parts.
func splitRepository(repository string) (owner, name string, err error) {
	owner, name, ok := strings.Cut(strings.TrimSpace(repository), "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return "", "", fmt.Errorf("repository %q must be in owner/name form", repository)
	}

	return owner, name, nil
}
