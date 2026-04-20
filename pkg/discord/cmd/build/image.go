package build

import (
	"fmt"
	"regexp"
	"strings"
)

// dockerImage is a combined-arch docker image produced by a manifest job.
type dockerImage struct {
	Repository string // e.g. "ethpandaops/prysm-beacon-chain"
	Tag        string // e.g. "stable" or "stable-minimal"
	Variant    string // human label derived from manifest job name, e.g. "" or "beacon-minimal"
}

// Reference returns "repository:tag".
func (i dockerImage) Reference() string {
	return fmt.Sprintf("%s:%s", i.Repository, i.Tag)
}

// HubURL returns the Docker Hub tag page URL.
func (i dockerImage) HubURL() string {
	return fmt.Sprintf("https://hub.docker.com/r/%s/tags?name=%s", i.Repository, i.Tag)
}

// Label returns a short user-facing label for dropdown options.
func (i dockerImage) Label() string {
	if i.Variant == "" {
		return i.Reference()
	}

	return fmt.Sprintf("%s (%s)", i.Reference(), i.Variant)
}

var (
	dockerTagInvalidChars  = regexp.MustCompile(`[^a-zA-Z0-9._]`)
	dockerTagLeadingDashes = regexp.MustCompile(`^-+`)
)

// computeBaseDockerTag mirrors the `docker-tag` composite action in
// eth-client-docker-image-builder. Given the workflow inputs and the upstream
// repo for this workflow, it returns the tag that `prepare.target_tag`
// produces at runtime.
func computeBaseDockerTag(repository, ref, dockerTagOverride, upstreamRepository string) string {
	input := dockerTagOverride
	if input == "" {
		input = ref
	}

	if input == "" {
		return ""
	}

	prefix := ""

	if upstreamRepository != "" && repository != "" && dockerTagOverride == "" && repository != upstreamRepository {
		if idx := strings.Index(repository, "/"); idx > 0 {
			prefix = repository[:idx] + "-"
		}
	}

	sanitized := dockerTagInvalidChars.ReplaceAllString(input, "-")
	sanitized = dockerTagLeadingDashes.ReplaceAllString(sanitized, "")

	return prefix + sanitized
}
