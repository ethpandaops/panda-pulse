package githubauth

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// DefaultBaseURL is the GitHub REST API base URL.
	DefaultBaseURL = "https://api.github.com"

	// tokenRefreshMargin is how long before expiry a cached installation
	// token is replaced. Installation tokens are valid for one hour.
	tokenRefreshMargin = 5 * time.Minute

	// installCheckTTL is how long the result of an installation lookup for a
	// repository is reused.
	installCheckTTL = 10 * time.Minute

	// jwtLifetime and jwtClockSkew bound the App JWT. GitHub rejects JWTs
	// that are valid for more than 10 minutes; iat is backdated to tolerate
	// clock drift.
	jwtLifetime  = 9 * time.Minute
	jwtClockSkew = 60 * time.Second

	maxErrorBodyBytes = 4096
)

// AppConfig configures GitHub App authentication.
type AppConfig struct {
	AppID          int64
	InstallationID int64
	PrivateKey     []byte // PEM-encoded RSA private key.
	BaseURL        string // Defaults to DefaultBaseURL.
}

// AppTokenSource mints GitHub App installation access tokens scoped to a
// single repository and permission set, and caches them until shortly
// before they expire.
type AppTokenSource struct {
	appID          int64
	installationID int64
	key            *rsa.PrivateKey
	baseURL        string
	httpClient     *http.Client
	now            func() time.Time

	// mu guards the maps below. It is never held during a network call.
	mu        sync.Mutex
	tokens    map[string]cachedToken
	installed map[string]installCheck
	// keyLocks serialises token creation per repository and permission set,
	// so concurrent callers for the same token share one request.
	keyLocks map[string]*sync.Mutex
}

type cachedToken struct {
	token     string
	expiresAt time.Time
}

type installCheck struct {
	installed bool
	checkedAt time.Time
}

// NewAppTokenSource creates an AppTokenSource.
func NewAppTokenSource(cfg AppConfig, httpClient *http.Client) (*AppTokenSource, error) {
	if cfg.AppID <= 0 {
		return nil, errors.New("GitHub App ID must be a positive integer")
	}

	if cfg.InstallationID <= 0 {
		return nil, errors.New("GitHub App installation ID must be a positive integer")
	}

	key, err := jwt.ParseRSAPrivateKeyFromPEM(cfg.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to parse GitHub App private key: %w", err)
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	return &AppTokenSource{
		appID:          cfg.AppID,
		installationID: cfg.InstallationID,
		key:            key,
		baseURL:        strings.TrimSuffix(baseURL, "/"),
		httpClient:     httpClient,
		now:            time.Now,
		tokens:         make(map[string]cachedToken),
		installed:      make(map[string]installCheck),
		keyLocks:       make(map[string]*sync.Mutex),
	}, nil
}

// Token returns an installation token limited to repository and perms. It
// returns an error wrapping ErrNotInstalled when the App's installation does
// not include repository.
func (s *AppTokenSource) Token(ctx context.Context, repository string, perms Permissions) (string, error) {
	owner, name, err := splitRepository(repository)
	if err != nil {
		return "", err
	}

	repoKey := strings.ToLower(owner + "/" + name)
	tokenKey := repoKey + "|" + permissionsKey(perms)

	keyLock := s.keyLock(tokenKey)

	keyLock.Lock()
	defer keyLock.Unlock()

	if token, ok := s.lookupToken(tokenKey); ok {
		return token, nil
	}

	installed, err := s.isInstalled(ctx, repoKey, owner, name)
	if err != nil {
		return "", err
	}

	if !installed {
		return "", fmt.Errorf("%w %s", ErrNotInstalled, owner+"/"+name)
	}

	t, err := s.createToken(ctx, name, perms)
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	s.tokens[tokenKey] = t
	s.mu.Unlock()

	return t.token, nil
}

// keyLock returns the lock that serialises token creation for key.
func (s *AppTokenSource) keyLock(key string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()

	l, ok := s.keyLocks[key]
	if !ok {
		l = &sync.Mutex{}
		s.keyLocks[key] = l
	}

	return l
}

// lookupToken returns the cached token for key unless it is within
// tokenRefreshMargin of expiry.
func (s *AppTokenSource) lookupToken(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tokens[key]
	if !ok || !s.now().Before(t.expiresAt.Add(-tokenRefreshMargin)) {
		return "", false
	}

	return t.token, true
}

// ReadToken returns an installation token when the App is installed on
// repository, and an empty token otherwise so the read is sent
// unauthenticated. Installation tokens only grant access to repositories in
// the installation, so they are not used for other public repositories.
func (s *AppTokenSource) ReadToken(ctx context.Context, repository string, perms Permissions) (string, error) {
	token, err := s.Token(ctx, repository, perms)
	if errors.Is(err, ErrNotInstalled) {
		return "", nil
	}

	return token, err
}

// isInstalled reports whether the configured installation covers owner/name.
func (s *AppTokenSource) isInstalled(ctx context.Context, repoKey, owner, name string) (bool, error) {
	s.mu.Lock()
	c, ok := s.installed[repoKey]
	s.mu.Unlock()

	if ok && s.now().Sub(c.checkedAt) < installCheckTTL {
		return c.installed, nil
	}

	endpoint := fmt.Sprintf("%s/repos/%s/%s/installation", s.baseURL, url.PathEscape(owner), url.PathEscape(name))

	resp, err := s.doAppRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, fmt.Errorf("failed to look up GitHub App installation for %s/%s: %w", owner, name, err)
	}

	defer resp.Body.Close()

	var installed bool

	switch resp.StatusCode {
	case http.StatusOK:
		var body struct {
			ID int64 `json:"id"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			return false, fmt.Errorf("failed to decode GitHub App installation for %s/%s: %w", owner, name, err)
		}

		installed = body.ID == s.installationID
	case http.StatusNotFound:
		installed = false
	default:
		return false, fmt.Errorf("GitHub App installation lookup for %s/%s failed: %s", owner, name, responseError(resp))
	}

	s.mu.Lock()
	s.installed[repoKey] = installCheck{installed: installed, checkedAt: s.now()}
	s.mu.Unlock()

	return installed, nil
}

// createToken requests an installation token limited to the named
// repository and perms.
func (s *AppTokenSource) createToken(ctx context.Context, name string, perms Permissions) (cachedToken, error) {
	reqBody, err := json.Marshal(struct {
		Repositories []string    `json:"repositories"`
		Permissions  Permissions `json:"permissions,omitempty"`
	}{
		Repositories: []string{name},
		Permissions:  perms,
	})
	if err != nil {
		return cachedToken{}, fmt.Errorf("failed to marshal token request: %w", err)
	}

	endpoint := fmt.Sprintf("%s/app/installations/%d/access_tokens", s.baseURL, s.installationID)

	resp, err := s.doAppRequest(ctx, http.MethodPost, endpoint, reqBody)
	if err != nil {
		return cachedToken{}, fmt.Errorf("failed to create GitHub App installation token: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return cachedToken{}, fmt.Errorf("failed to create GitHub App installation token: %s", responseError(resp))
	}

	//nolint:tagliatelle // GitHub defined structure.
	var body struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return cachedToken{}, fmt.Errorf("failed to decode GitHub App installation token: %w", err)
	}

	if body.Token == "" {
		return cachedToken{}, errors.New("GitHub returned an empty installation token")
	}

	return cachedToken{token: body.Token, expiresAt: body.ExpiresAt}, nil
}

// doAppRequest sends a request authenticated as the App itself.
func (s *AppTokenSource) doAppRequest(ctx context.Context, method, endpoint string, body []byte) (*http.Response, error) {
	signed, err := s.signJWT()
	if err != nil {
		return nil, err
	}

	var reader io.Reader = http.NoBody
	if body != nil {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+signed)

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return s.httpClient.Do(req)
}

// signJWT returns an RS256 JWT identifying the App.
func (s *AppTokenSource) signJWT() (string, error) {
	now := s.now()

	signed, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{
		Issuer:    strconv.FormatInt(s.appID, 10),
		IssuedAt:  jwt.NewNumericDate(now.Add(-jwtClockSkew)),
		ExpiresAt: jwt.NewNumericDate(now.Add(jwtLifetime)),
	}).SignedString(s.key)
	if err != nil {
		return "", fmt.Errorf("failed to sign GitHub App JWT: %w", err)
	}

	return signed, nil
}

// permissionsKey returns a stable cache key for perms.
func permissionsKey(perms Permissions) string {
	parts := make([]string, 0, len(perms))

	for _, name := range slices.Sorted(maps.Keys(perms)) {
		parts = append(parts, name+"="+perms[name])
	}

	return strings.Join(parts, ",")
}

// responseError describes a failed GitHub response using its status and the
// "message" field of the error body, if any.
func responseError(resp *http.Response) string {
	var body struct {
		Message string `json:"message"`
	}

	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	if json.Unmarshal(data, &body) == nil && body.Message != "" {
		return fmt.Sprintf("status %d: %s", resp.StatusCode, body.Message)
	}

	return fmt.Sprintf("status %d", resp.StatusCode)
}
