package build

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/bwmarrin/discordgo"
)

// prOption returns the shared slash-command option used to override
// repository/ref from a GitHub PR URL.
func prOption() *discordgo.ApplicationCommandOption {
	return &discordgo.ApplicationCommandOption{
		Name:        optionPR,
		Description: "PR URL or owner/repo#N — auto-detects repository & ref",
		Type:        discordgo.ApplicationCommandOptionString,
		Required:    false,
	}
}

// prResolution is the outcome of resolving a PR reference: the head repository
// full name (e.g. "sigp/lighthouse") and the head ref (branch name) that the
// workflow should check out.
type prResolution struct {
	Repository string
	Ref        string
}

// pullResponse is the subset of the GitHub Pulls API response we consume.
//
//nolint:tagliatelle // Github defined structure.
type pullResponse struct {
	Head struct {
		Ref  string `json:"ref"`
		Repo struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"head"`
}

// prURLPattern matches a GitHub PR URL (with or without scheme) and captures
// owner, repo, and PR number.
var prURLPattern = regexp.MustCompile(`(?i)^(?:https?://)?(?:www\.)?github\.com/([^/]+)/([^/]+)/pull/(\d+)(?:[/?#].*)?$`)

// prShortPattern matches "owner/repo#N".
var prShortPattern = regexp.MustCompile(`^([^/\s]+)/([^/#\s]+)#(\d+)$`)

// prBarePattern matches "#N" or "N".
var prBarePattern = regexp.MustCompile(`^#?(\d+)$`)

// parsePRReference extracts owner, repo, and PR number from a user-provided
// string. If the input is a bare number, fallbackRepo (expected in
// "owner/repo" form) is used. Returns an error when the input is empty or
// doesn't match any supported format.
func parsePRReference(input, fallbackRepo string) (owner, repo string, number string, err error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", "", "", fmt.Errorf("pr reference is empty")
	}

	if m := prURLPattern.FindStringSubmatch(trimmed); m != nil {
		return m[1], strings.TrimSuffix(m[2], ".git"), m[3], nil
	}

	if m := prShortPattern.FindStringSubmatch(trimmed); m != nil {
		return m[1], m[2], m[3], nil
	}

	if m := prBarePattern.FindStringSubmatch(trimmed); m != nil {
		if fallbackRepo == "" {
			return "", "", "", fmt.Errorf("bare PR number requires a known upstream repo; paste a full URL instead")
		}

		parts := strings.SplitN(fallbackRepo, "/", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return "", "", "", fmt.Errorf("fallback repo %q is not owner/repo", fallbackRepo)
		}

		return parts[0], parts[1], m[1], nil
	}

	return "", "", "", fmt.Errorf("unrecognised PR reference: %q (use a full URL or owner/repo#N)", trimmed)
}

// prFallbackRepo returns the owner/repo that should be used when the user
// provides a bare PR number (e.g. "123"). It maps the target to the default
// repository defined by the client's workflow_dispatch inputs. Returns an
// empty string when the workflow lookup fails — the caller then rejects bare
// numbers and requires a full URL.
func (c *BuildCommand) prFallbackRepo(targetName string) string {
	allWorkflows, err := c.workflowFetcher.GetAllWorkflows()
	if err != nil {
		return ""
	}

	workflowName := getClientToWorkflowName(targetName)

	workflow, exists := allWorkflows[workflowName]
	if !exists {
		return ""
	}

	return workflow.Repository
}

// resolvePR fetches the PR from GitHub and returns the head repository and
// head ref that the workflow should build from. PRs from forks resolve to the
// fork's full name so the workflow can check out the fork branch.
func (c *BuildCommand) resolvePR(input, fallbackRepo string) (*prResolution, error) {
	owner, repo, number, err := parsePRReference(input, fallbackRepo)
	if err != nil {
		return nil, err
	}

	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%s", url.PathEscape(owner), url.PathEscape(repo), number)

	req, err := http.NewRequest(http.MethodGet, apiURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to build PR request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github.v3+json")

	if c.githubToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.githubToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch PR: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("PR %s/%s#%s not found", owner, repo, number)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d for PR %s/%s#%s", resp.StatusCode, owner, repo, number)
	}

	var pr pullResponse
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return nil, fmt.Errorf("failed to decode PR response: %w", err)
	}

	if pr.Head.Repo.FullName == "" || pr.Head.Ref == "" {
		return nil, fmt.Errorf("PR %s/%s#%s has no head repo/ref (deleted fork?)", owner, repo, number)
	}

	return &prResolution{
		Repository: pr.Head.Repo.FullName,
		Ref:        pr.Head.Ref,
	}, nil
}
