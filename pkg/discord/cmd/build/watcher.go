package build

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/sirupsen/logrus"
)

const (
	// pollInterval is how often we check GitHub for status changes of tracked runs.
	pollInterval = 30 * time.Second
	// claimTimeout is how long we keep polling for the run ID after a dispatch.
	claimTimeout = 90 * time.Second
	// claimPollInterval is how often we poll the runs list while claiming.
	claimPollInterval = 2 * time.Second
	// runTimeout is an upper bound after which we stop watching a build.
	runTimeout = 3 * time.Hour
)

// ClaimRequest captures everything the watcher needs to resolve the set of
// combined-arch images a build produces and DM the invoker on completion.
type ClaimRequest struct {
	UserID          string
	TargetDisplay   string
	WorkflowFile    string
	CorrelationID   string
	HTMLURL         string
	RepositoryInput string
	RefInput        string
	DockerTagInput  string
	UpstreamRepo    string
	Manifests       []ManifestInfo
}

// trackedBuild is a dispatched build we're watching for completion.
type trackedBuild struct {
	userID          string
	targetDisplay   string
	workflowFile    string
	correlationID   string
	runID           int64
	htmlURL         string
	dispatchedAt    time.Time
	repositoryInput string
	refInput        string
	dockerTagInput  string
	upstreamRepo    string
	manifests       []ManifestInfo
}

// workflowRun is the subset of the GitHub workflow run payload we use.
//
//nolint:tagliatelle // Github defined structure.
type workflowRun struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	DisplayTitle string `json:"display_title"`
	Status       string `json:"status"`
	Conclusion   string `json:"conclusion"`
	HTMLURL      string `json:"html_url"`
}

// workflowRunsResponse wraps the list-runs endpoint response.
//
//nolint:tagliatelle // Github defined structure.
type workflowRunsResponse struct {
	WorkflowRuns []workflowRun `json:"workflow_runs"`
}

// workflowJobResult is the subset of a workflow run job we use.
type workflowJobResult struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

// workflowJobsResponse wraps the list-jobs endpoint response.
type workflowJobsResponse struct {
	Jobs []workflowJobResult `json:"jobs"`
}

// BuildWatcher tracks dispatched builds and DMs the invoker on completion.
type BuildWatcher struct {
	log         logrus.FieldLogger
	session     *discordgo.Session
	httpClient  *http.Client
	githubToken string

	mu     sync.Mutex
	tracks map[int64]*trackedBuild

	wg sync.WaitGroup
}

// NewBuildWatcher creates a new BuildWatcher.
func NewBuildWatcher(log logrus.FieldLogger, session *discordgo.Session, client *http.Client, githubToken string) *BuildWatcher {
	return &BuildWatcher{
		log:         log.WithField("component", "build/watcher"),
		session:     session,
		httpClient:  client,
		githubToken: githubToken,
		tracks:      make(map[int64]*trackedBuild, 8),
	}
}

// Start launches the poller goroutine. The goroutine exits when ctx is cancelled.
func (w *BuildWatcher) Start(ctx context.Context) {
	w.wg.Add(1)

	go w.pollLoop(ctx)
}

// Wait blocks until the poller goroutine has exited.
func (w *BuildWatcher) Wait() {
	w.wg.Wait()
}

// Claim finds the workflow_dispatch run matching the given correlation ID and
// starts tracking it for completion notifications. The returned URL is the
// run's html_url (or the dispatch fallback if GitHub didn't surface one),
// suitable for editing the triggered message to point at the specific run
// rather than the workflow file listing.
func (w *BuildWatcher) Claim(ctx context.Context, req ClaimRequest) (string, error) {
	if req.CorrelationID == "" {
		return "", fmt.Errorf("correlation id is required")
	}

	claimCtx, cancel := context.WithTimeout(ctx, claimTimeout)
	defer cancel()

	ticker := time.NewTicker(claimPollInterval)
	defer ticker.Stop()

	for {
		run, err := w.findRunByCorrelation(claimCtx, req.WorkflowFile, req.CorrelationID)
		if err != nil {
			w.log.WithError(err).WithField("workflow", req.WorkflowFile).Debug("Failed to list runs, retrying")
		} else if run != nil {
			runURL := run.HTMLURL
			if runURL == "" {
				runURL = req.HTMLURL
			}

			w.track(run, req)

			return runURL, nil
		}

		select {
		case <-claimCtx.Done():
			return "", fmt.Errorf("timed out claiming run for %s (correlation %s)", req.WorkflowFile, req.CorrelationID)
		case <-ticker.C:
		}
	}
}

func (w *BuildWatcher) track(run *workflowRun, req ClaimRequest) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, exists := w.tracks[run.ID]; exists {
		return
	}

	htmlURL := run.HTMLURL
	if htmlURL == "" {
		htmlURL = req.HTMLURL
	}

	w.tracks[run.ID] = &trackedBuild{
		userID:          req.UserID,
		targetDisplay:   req.TargetDisplay,
		workflowFile:    req.WorkflowFile,
		correlationID:   req.CorrelationID,
		runID:           run.ID,
		htmlURL:         htmlURL,
		dispatchedAt:    time.Now(),
		repositoryInput: req.RepositoryInput,
		refInput:        req.RefInput,
		dockerTagInput:  req.DockerTagInput,
		upstreamRepo:    req.UpstreamRepo,
		manifests:       req.Manifests,
	}

	w.log.WithFields(logrus.Fields{
		"run_id":         run.ID,
		"user":           req.UserID,
		"target":         req.TargetDisplay,
		"workflow":       req.WorkflowFile,
		"correlation_id": req.CorrelationID,
	}).Info("Tracking build for completion DM")
}

// findRunByCorrelation returns the workflow_dispatch run whose run-name
// contains the given correlation ID, if any.
func (w *BuildWatcher) findRunByCorrelation(ctx context.Context, workflowFile, correlationID string) (*workflowRun, error) {
	url := fmt.Sprintf(
		"https://api.github.com/repos/%s/actions/workflows/%s/runs?event=workflow_dispatch&per_page=20",
		DefaultRepository, workflowFile,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("Authorization", "Bearer "+w.githubToken)

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list runs status %d", resp.StatusCode)
	}

	var body workflowRunsResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode runs: %w", err)
	}

	for i := range body.WorkflowRuns {
		run := body.WorkflowRuns[i]
		if runHasCorrelation(run, correlationID) {
			return &run, nil
		}
	}

	return nil, nil //nolint:nilnil // no match yet is a normal retry signal.
}

func runHasCorrelation(run workflowRun, correlationID string) bool {
	return strings.Contains(run.Name, correlationID) || strings.Contains(run.DisplayTitle, correlationID)
}

func (w *BuildWatcher) pollLoop(ctx context.Context) {
	defer w.wg.Done()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.tickOnce(ctx)
		}
	}
}

func (w *BuildWatcher) tickOnce(ctx context.Context) {
	w.mu.Lock()

	snapshot := make([]*trackedBuild, 0, len(w.tracks))
	for _, b := range w.tracks {
		snapshot = append(snapshot, b)
	}

	w.mu.Unlock()

	for _, b := range snapshot {
		if time.Since(b.dispatchedAt) > runTimeout {
			w.finalize(ctx, b, "timed_out")

			continue
		}

		run, err := w.fetchRun(ctx, b.runID)
		if err != nil {
			w.log.WithError(err).WithField("run_id", b.runID).Warn("Failed to fetch run status")

			continue
		}

		if run.Status != "completed" {
			continue
		}

		if run.HTMLURL != "" {
			b.htmlURL = run.HTMLURL
		}

		w.finalize(ctx, b, run.Conclusion)
	}

}

func (w *BuildWatcher) finalize(ctx context.Context, b *trackedBuild, conclusion string) {
	images := w.resolveImages(ctx, b)

	w.notify(b, conclusion, images)

	w.mu.Lock()
	delete(w.tracks, b.runID)
	w.mu.Unlock()
}

// resolveImages fetches the workflow run jobs and returns the combined-arch
// images that correspond to manifest jobs which completed successfully.
func (w *BuildWatcher) resolveImages(ctx context.Context, b *trackedBuild) []dockerImage {
	if len(b.manifests) == 0 {
		return nil
	}

	jobs, err := w.fetchJobs(ctx, b.runID)
	if err != nil {
		w.log.WithError(err).WithField("run_id", b.runID).Warn("Failed to fetch run jobs, skipping image resolution")

		return nil
	}

	jobByName := make(map[string]workflowJobResult, len(jobs))
	for _, j := range jobs {
		jobByName[j.Name] = j
	}

	baseTag := computeBaseDockerTag(b.repositoryInput, b.refInput, b.dockerTagInput, b.upstreamRepo)
	if baseTag == "" {
		return nil
	}

	images := make([]dockerImage, 0, len(b.manifests))

	for _, m := range b.manifests {
		job, ok := jobByName[m.JobName]
		if !ok || job.Conclusion != "success" {
			continue
		}

		images = append(images, dockerImage{
			Repository: m.Repository,
			Tag:        baseTag + m.TagSuffix,
			Variant:    m.Variant,
		})
	}

	return images
}

func (w *BuildWatcher) fetchRun(ctx context.Context, runID int64) (*workflowRun, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/actions/runs/%d", DefaultRepository, runID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("Authorization", "Bearer "+w.githubToken)

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get run: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get run status %d", resp.StatusCode)
	}

	var run workflowRun
	if err := json.NewDecoder(resp.Body).Decode(&run); err != nil {
		return nil, fmt.Errorf("decode run: %w", err)
	}

	return &run, nil
}

func (w *BuildWatcher) fetchJobs(ctx context.Context, runID int64) ([]workflowJobResult, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/actions/runs/%d/jobs?per_page=100", DefaultRepository, runID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("Authorization", "Bearer "+w.githubToken)

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list jobs status %d", resp.StatusCode)
	}

	var body workflowJobsResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode jobs: %w", err)
	}

	return body.Jobs, nil
}

func (w *BuildWatcher) notify(b *trackedBuild, conclusion string, images []dockerImage) {
	channel, err := w.session.UserChannelCreate(b.userID)
	if err != nil {
		w.log.WithError(err).WithField("user", b.userID).Warn("Failed to create DM channel")

		return
	}

	embed := buildCompletionEmbed(b, conclusion, images)

	send := &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{embed},
	}

	if _, err := w.session.ChannelMessageSendComplex(channel.ID, send); err != nil {
		w.log.WithError(err).WithField("user", b.userID).Warn("Failed to send build completion DM")
	}
}

// buildCompletionEmbed builds the DM embed shown to the invoker. Image tags
// are inlined as fenced code blocks so each can be copied via Discord's
// per-block copy icon, and are grouped into main and minimal sections.
func buildCompletionEmbed(b *trackedBuild, conclusion string, images []dockerImage) *discordgo.MessageEmbed {
	embed := &discordgo.MessageEmbed{
		Title: fmt.Sprintf("%s Build %s: %s", conclusionEmoji(conclusion), conclusionLabel(conclusion), b.targetDisplay),
		Color: buildEmbedColor,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "Run",
				Value:  fmt.Sprintf("[View on GitHub](%s)", b.htmlURL),
				Inline: false,
			},
			{
				Name:   "Duration",
				Value:  time.Since(b.dispatchedAt).Truncate(time.Second).String(),
				Inline: true,
			},
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}

	main, minimal := partitionImages(images)

	if value := formatImageBlocks(main); value != "" {
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   "Images",
			Value:  value,
			Inline: false,
		})
	}

	if value := formatImageBlocks(minimal); value != "" {
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   "Minimal images",
			Value:  value,
			Inline: false,
		})
	}

	return embed
}

// partitionImages splits images into main and minimal groups based on whether
// the manifest variant carries the "minimal" marker.
func partitionImages(images []dockerImage) (main, minimal []dockerImage) {
	main = make([]dockerImage, 0, len(images))
	minimal = make([]dockerImage, 0, len(images))

	for _, img := range images {
		if strings.Contains(img.Variant, "minimal") {
			minimal = append(minimal, img)
		} else {
			main = append(main, img)
		}
	}

	return main, minimal
}

// formatImageBlocks renders each image as its own fenced code block so
// Discord shows a per-block copy icon on hover.
func formatImageBlocks(images []dockerImage) string {
	if len(images) == 0 {
		return ""
	}

	var buf strings.Builder

	buf.Grow(len(images) * 64)

	for idx, img := range images {
		if idx > 0 {
			buf.WriteByte('\n')
		}

		fmt.Fprintf(&buf, "```\n%s\n```", img.Reference())
	}

	return buf.String()
}

func conclusionEmoji(conclusion string) string {
	switch conclusion {
	case "success":
		return "✅"
	case "failure":
		return "❌"
	case "cancelled":
		return "🚫"
	case "timed_out":
		return "⏱️"
	case "action_required":
		return "⚠️"
	default:
		return "ℹ️"
	}
}

func conclusionLabel(conclusion string) string {
	if conclusion == "" {
		return "completed"
	}

	return conclusion
}
