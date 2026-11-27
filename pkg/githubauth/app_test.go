package githubauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testAppID          = 12345
	testInstallationID = 42
	testRepo           = "ethpandaops/eth-client-docker-image-builder"
)

// fakeGitHub serves the GitHub App endpoints used by AppTokenSource and
// verifies every request carries a JWT signed by the App's key.
type fakeGitHub struct {
	t   *testing.T
	key *rsa.PrivateKey

	mu            sync.Mutex
	now           time.Time
	installations map[string]int64 // "owner/name" -> installation ID.
	tokenBodies   []string
	lookups       int
	tokenStatus   int
	tokenMessage  string
}

func newFakeGitHub(t *testing.T) (*fakeGitHub, *httptest.Server) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	f := &fakeGitHub{
		t:             t,
		key:           key,
		now:           time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		installations: map[string]int64{testRepo: testInstallationID},
		tokenStatus:   http.StatusCreated,
	}

	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)

	return f, srv
}

func (f *fakeGitHub) clock() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.now
}

func (f *fakeGitHub) advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.now = f.now.Add(d)
}

func (f *fakeGitHub) privateKeyPEM() []byte {
	return pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(f.key),
	})
}

// verifyJWT checks that the request is authenticated with a JWT for the App,
// signed with the App's private key and accepted at the fake clock's time.
func (f *fakeGitHub) verifyJWT(r *http.Request) error {
	raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return fmt.Errorf("missing bearer JWT")
	}

	claims := &jwt.RegisteredClaims{}

	_, err := jwt.ParseWithClaims(raw, claims, func(*jwt.Token) (any, error) {
		return &f.key.PublicKey, nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithIssuer(fmt.Sprint(testAppID)),
		jwt.WithIssuedAt(),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(f.clock),
	)
	if err != nil {
		return err
	}

	if lifetime := claims.ExpiresAt.Sub(claims.IssuedAt.Time); lifetime > 10*time.Minute {
		return fmt.Errorf("JWT lifetime %s exceeds 10 minutes", lifetime)
	}

	return nil
}

func (f *fakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	if err := f.verifyJWT(r); err != nil {
		f.t.Errorf("invalid JWT on %s %s: %v", r.Method, r.URL.Path, err)
		http.Error(w, `{"message":"bad JWT"}`, http.StatusUnauthorized)

		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	switch {
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/installation"):
		f.lookups++

		repo := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/repos/"), "/installation")

		id, ok := f.installations[repo]
		if !ok {
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)

			return
		}

		_ = json.NewEncoder(w).Encode(map[string]any{"id": id})
	case r.Method == http.MethodPost && r.URL.Path == fmt.Sprintf("/app/installations/%d/access_tokens", testInstallationID):
		body, _ := io.ReadAll(r.Body)
		f.tokenBodies = append(f.tokenBodies, string(body))

		if f.tokenStatus != http.StatusCreated {
			w.WriteHeader(f.tokenStatus)
			_ = json.NewEncoder(w).Encode(map[string]string{"message": f.tokenMessage})

			return
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":      fmt.Sprintf("ghs_test_%d", len(f.tokenBodies)),
			"expires_at": f.now.Add(time.Hour).Format(time.RFC3339),
		})
	default:
		f.t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	}
}

func (f *fakeGitHub) mints() int {
	return len(f.bodies())
}

func (f *fakeGitHub) bodies() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.tokenBodies...)
}

func (f *fakeGitHub) lookupCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.lookups
}

func newTestSource(t *testing.T, f *fakeGitHub, srv *httptest.Server) *AppTokenSource {
	t.Helper()

	src, err := NewAppTokenSource(AppConfig{
		AppID:          testAppID,
		InstallationID: testInstallationID,
		PrivateKey:     f.privateKeyPEM(),
		BaseURL:        srv.URL,
	}, srv.Client())
	require.NoError(t, err)

	src.now = f.clock

	return src
}

func TestAppTokenSourceSignsJWTWithAppKey(t *testing.T) {
	f, srv := newFakeGitHub(t)
	src := newTestSource(t, f, srv)

	// The fake server fails the test if either request lacks a JWT that
	// verifies against the App's public key.
	token, err := src.Token(context.Background(), testRepo, ActionsWrite)
	require.NoError(t, err)
	assert.Equal(t, "ghs_test_1", token)

	signed, err := src.signJWT()
	require.NoError(t, err)

	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	_, err = jwt.Parse(signed, func(*jwt.Token) (any, error) {
		return &otherKey.PublicKey, nil
	}, jwt.WithTimeFunc(f.clock))
	require.ErrorIs(t, err, jwt.ErrTokenSignatureInvalid)
}

func TestAppTokenSourceScopesTokenToRepositoryAndPermissions(t *testing.T) {
	f, srv := newFakeGitHub(t)
	src := newTestSource(t, f, srv)

	_, err := src.Token(context.Background(), testRepo, ActionsWrite)
	require.NoError(t, err)

	_, err = src.Token(context.Background(), testRepo, ContentsRead)
	require.NoError(t, err)

	bodies := f.bodies()
	require.Len(t, bodies, 2)
	assert.JSONEq(t, `{"repositories":["eth-client-docker-image-builder"],"permissions":{"actions":"write"}}`, bodies[0])
	assert.JSONEq(t, `{"repositories":["eth-client-docker-image-builder"],"permissions":{"contents":"read"}}`, bodies[1])
}

func TestAppTokenSourceCachesAndRefreshesBeforeExpiry(t *testing.T) {
	f, srv := newFakeGitHub(t)
	src := newTestSource(t, f, srv)
	ctx := context.Background()

	first, err := src.Token(ctx, testRepo, ActionsRead)
	require.NoError(t, err)

	// Same repository and permissions: served from cache, regardless of case.
	again, err := src.Token(ctx, "EthPandaOps/Eth-Client-Docker-Image-Builder", ActionsRead)
	require.NoError(t, err)
	assert.Equal(t, first, again)
	assert.Equal(t, 1, f.mints())

	// More than five minutes before expiry: still cached.
	f.advance(54 * time.Minute)

	again, err = src.Token(ctx, testRepo, ActionsRead)
	require.NoError(t, err)
	assert.Equal(t, first, again)
	assert.Equal(t, 1, f.mints())

	// Within five minutes of expiry: replaced with a new token.
	f.advance(2 * time.Minute)

	refreshed, err := src.Token(ctx, testRepo, ActionsRead)
	require.NoError(t, err)
	assert.NotEqual(t, first, refreshed)
	assert.Equal(t, 2, f.mints())

	// The refreshed token is cached in turn.
	again, err = src.Token(ctx, testRepo, ActionsRead)
	require.NoError(t, err)
	assert.Equal(t, refreshed, again)
	assert.Equal(t, 2, f.mints())
}

func TestAppTokenSourceNotInstalled(t *testing.T) {
	f, srv := newFakeGitHub(t)
	f.installations["ethpandaops/other-installation"] = testInstallationID + 1
	src := newTestSource(t, f, srv)
	ctx := context.Background()

	for _, repo := range []string{"sigp/lighthouse", "ethpandaops/other-installation"} {
		_, err := src.Token(ctx, repo, ActionsWrite)
		require.ErrorIs(t, err, ErrNotInstalled)
		assert.EqualError(t, err, "GitHub App is not installed on "+repo)

		token, err := src.ReadToken(ctx, repo, PullRequestsRead)
		require.NoError(t, err)
		assert.Empty(t, token)
	}

	assert.Equal(t, 0, f.mints())
	assert.Equal(t, 2, f.lookupCount(), "installation lookups are cached")

	// ReadToken returns an installation token where the App is installed.
	token, err := src.ReadToken(ctx, testRepo, ContentsRead)
	require.NoError(t, err)
	assert.Equal(t, "ghs_test_1", token)
}

func TestAppTokenSourceTokenRequestFailure(t *testing.T) {
	f, srv := newFakeGitHub(t)
	f.tokenStatus = http.StatusUnprocessableEntity
	f.tokenMessage = "The permissions requested are not granted to this installation."
	src := newTestSource(t, f, srv)

	_, err := src.Token(context.Background(), testRepo, ActionsWrite)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrNotInstalled)
	assert.Contains(t, err.Error(), "status 422: The permissions requested are not granted to this installation.")
}

func TestAppTokenSourceRejectsInvalidRepository(t *testing.T) {
	f, srv := newFakeGitHub(t)
	src := newTestSource(t, f, srv)

	for _, repo := range []string{"", "lighthouse", "sigp/", "/lighthouse", "a/b/c"} {
		_, err := src.Token(context.Background(), repo, ActionsWrite)
		require.Error(t, err, repo)
	}

	assert.Equal(t, 0, f.lookupCount())
}

func TestNewAppTokenSourceValidatesConfig(t *testing.T) {
	f, _ := newFakeGitHub(t)

	_, err := NewAppTokenSource(AppConfig{InstallationID: 1, PrivateKey: f.privateKeyPEM()}, nil)
	require.Error(t, err)

	_, err = NewAppTokenSource(AppConfig{AppID: 1, PrivateKey: f.privateKeyPEM()}, nil)
	require.Error(t, err)

	_, err = NewAppTokenSource(AppConfig{AppID: 1, InstallationID: 1, PrivateKey: []byte("not a key")}, nil)
	require.Error(t, err)
}

func TestStaticTokenIsUsedForEveryRequest(t *testing.T) {
	tokens := StaticToken("pat")
	ctx := context.Background()

	for _, repo := range []string{testRepo, "sigp/lighthouse"} {
		token, err := tokens.Token(ctx, repo, ActionsWrite)
		require.NoError(t, err)
		assert.Equal(t, "pat", token)

		token, err = tokens.ReadToken(ctx, repo, PullRequestsRead)
		require.NoError(t, err)
		assert.Equal(t, "pat", token)
	}

	req := httptest.NewRequest(http.MethodGet, "https://api.github.com/", http.NoBody)
	require.NoError(t, SetAuthorization(ctx, req, tokens, testRepo, ActionsWrite))
	assert.Equal(t, "Bearer pat", req.Header.Get("Authorization"))

	req = httptest.NewRequest(http.MethodGet, "https://api.github.com/", http.NoBody)
	require.NoError(t, SetReadAuthorization(ctx, req, tokens, "sigp/lighthouse", PullRequestsRead))
	assert.Equal(t, "Bearer pat", req.Header.Get("Authorization"))
}

func TestSetReadAuthorizationWithoutInstallationSendsNoHeader(t *testing.T) {
	f, srv := newFakeGitHub(t)
	src := newTestSource(t, f, srv)

	req := httptest.NewRequest(http.MethodGet, "https://api.github.com/", http.NoBody)
	require.NoError(t, SetReadAuthorization(context.Background(), req, src, "sigp/lighthouse", PullRequestsRead))
	assert.Empty(t, req.Header.Get("Authorization"))
}
