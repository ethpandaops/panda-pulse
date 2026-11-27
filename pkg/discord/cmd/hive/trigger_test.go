package hive

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ethpandaops/panda-pulse/pkg/githubauth"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingTransport records requests and answers each with 204 No Content.
type recordingTransport struct {
	requests []*http.Request
}

func (rt *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.requests = append(rt.requests, req)

	return &http.Response{
		StatusCode: http.StatusNoContent,
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    req,
	}, nil
}

// notInstalledTokens is a TokenSource for an App installed on no repository.
type notInstalledTokens struct{}

func (notInstalledTokens) Token(_ context.Context, repository string, _ githubauth.Permissions) (string, error) {
	return "", fmt.Errorf("%w %s", githubauth.ErrNotInstalled, repository)
}

func (notInstalledTokens) ReadToken(_ context.Context, _ string, _ githubauth.Permissions) (string, error) {
	return "", nil
}

func TestTriggerHiveWorkflowWithToken(t *testing.T) {
	rt := &recordingTransport{}
	cmd := NewHiveCommand(logrus.New(), nil, githubauth.StaticToken("pat"), &http.Client{Transport: rt})

	require.NoError(t, cmd.triggerHiveWorkflow("ethpandaops/bal-devnets", "hive.yaml", "master", "geth"))

	require.Len(t, rt.requests, 1)
	assert.Equal(t, "https://api.github.com/repos/ethpandaops/bal-devnets/actions/workflows/hive.yaml/dispatches", rt.requests[0].URL.String())
	assert.Equal(t, "Bearer pat", rt.requests[0].Header.Get("Authorization"))
}

func TestTriggerHiveWorkflowAppNotInstalled(t *testing.T) {
	rt := &recordingTransport{}
	cmd := NewHiveCommand(logrus.New(), nil, notInstalledTokens{}, &http.Client{Transport: rt})

	err := cmd.triggerHiveWorkflow("ethpandaops/bal-devnets", "hive.yaml", "master", "geth")
	require.ErrorIs(t, err, githubauth.ErrNotInstalled)
	assert.EqualError(t, err, "GitHub App is not installed on ethpandaops/bal-devnets")
	assert.Empty(t, rt.requests)
}
