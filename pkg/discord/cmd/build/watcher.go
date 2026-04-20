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

// trackedBuild is a dispatched build we're watching for completion.
type trackedBuild struct {
	userID        string
	targetDisplay string
	workflowFile  string
	correlationID string
	runID         int64
	htmlURL       string
	dispatchedAt  time.Time
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

// Claim finds the workflow_dispatch run for the given workflowFile whose
// run-name contains correlationID, and starts tracking it for completion
// notifications.
func (w *BuildWatcher) Claim(ctx context.Context, userID, targetDisplay, workflowFile, correlationID, htmlURL string) error {
	if correlationID == "" {
		return fmt.Errorf("correlation id is required")
	}

	claimCtx, cancel := context.WithTimeout(ctx, claimTimeout)
	defer cancel()

	ticker := time.NewTicker(claimPollInterval)
	defer ticker.Stop()

	for {
		run, err := w.findRunByCorrelation(claimCtx, workflowFile, correlationID)
		if err != nil {
			w.log.WithError(err).WithField("workflow", workflowFile).Debug("Failed to list runs, retrying")
		} else if run != nil {
			w.track(run, userID, targetDisplay, workflowFile, correlationID, htmlURL)

			return nil
		}

		select {
		case <-claimCtx.Done():
			return fmt.Errorf("timed out claiming run for %s (correlation %s)", workflowFile, correlationID)
		case <-ticker.C:
		}
	}
}

func (w *BuildWatcher) track(run *workflowRun, userID, targetDisplay, workflowFile, correlationID, fallbackURL string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, exists := w.tracks[run.ID]; exists {
		return
	}

	htmlURL := run.HTMLURL
	if htmlURL == "" {
		htmlURL = fallbackURL
	}

	w.tracks[run.ID] = &trackedBuild{
		userID:        userID,
		targetDisplay: targetDisplay,
		workflowFile:  workflowFile,
		correlationID: correlationID,
		runID:         run.ID,
		htmlURL:       htmlURL,
		dispatchedAt:  time.Now(),
	}

	w.log.WithFields(logrus.Fields{
		"run_id":         run.ID,
		"user":           userID,
		"target":         targetDisplay,
		"workflow":       workflowFile,
		"correlation_id": correlationID,
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
			w.notify(b, "timed_out")
			w.untrack(b.runID)

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

		w.notify(b, run.Conclusion)
		w.untrack(b.runID)
	}
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

func (w *BuildWatcher) untrack(runID int64) {
	w.mu.Lock()
	defer w.mu.Unlock()

	delete(w.tracks, runID)
}

func (w *BuildWatcher) notify(b *trackedBuild, conclusion string) {
	channel, err := w.session.UserChannelCreate(b.userID)
	if err != nil {
		w.log.WithError(err).WithField("user", b.userID).Warn("Failed to create DM channel")

		return
	}

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

	if _, err := w.session.ChannelMessageSendEmbed(channel.ID, embed); err != nil {
		w.log.WithError(err).WithField("user", b.userID).Warn("Failed to send build completion DM")
	}
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
