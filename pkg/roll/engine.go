package roll

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
)

// Options tunes a rollout.
type Options struct {
	// Image scopes the roll to a single image (empty = all watched containers).
	Image string
	// DelayBetweenNodes is the wait after a node recovers before the next.
	DelayBetweenNodes time.Duration
	// PostTriggerWait is the grace period after triggering before polling recovery.
	PostTriggerWait time.Duration
	// WaitTimeout is the per-node recovery timeout; exceeding it aborts the rollout.
	WaitTimeout time.Duration
	// HealthCheckInterval is how often to poll beacon health while waiting.
	HealthCheckInterval time.Duration
	// MaxSyncDistance is the largest sync distance (slots) still considered healthy.
	MaxSyncDistance uint64
	// SkipHealth disables beacon health gating (trigger-and-go).
	SkipHealth bool
	// DryRun logs intent without triggering rolls.
	DryRun bool
	// DoraURL, if set, makes health checks use Dora (matched by node name) as the
	// source of truth instead of per-node beacon calls. Preferred — one
	// unauthenticated call covers the fleet.
	DoraURL string
	// BeaconBasicAuthUser and BeaconBasicAuthPass authenticate beacon health
	// checks when Dora is not used and the beacon is behind nginx basic auth.
	BeaconBasicAuthUser string
	BeaconBasicAuthPass string
	// OnProgress, if set, is invoked at each rollout milestone (for UIs such as
	// the Discord command). It must be cheap and non-blocking.
	OnProgress func(Progress)
}

// Phase is a rollout milestone for progress reporting.
type Phase string

const (
	PhaseTriggering Phase = "triggering"
	PhaseHealthy    Phase = "healthy"
	PhaseFailed     Phase = "failed"
	PhaseSkipped    Phase = "skipped"
	PhaseDone       Phase = "done"
)

// Progress is a rollout milestone delivered to Options.OnProgress.
type Progress struct {
	Node    string
	Index   int // 1-based; 0 for fleet-level events
	Total   int
	Phase   Phase
	Message string
}

func (o *Options) applyDefaults() {
	if o.DelayBetweenNodes == 0 {
		o.DelayBetweenNodes = time.Minute
	}
	if o.PostTriggerWait == 0 {
		o.PostTriggerWait = 30 * time.Second
	}
	if o.WaitTimeout == 0 {
		o.WaitTimeout = 10 * time.Minute
	}
	if o.HealthCheckInterval == 0 {
		o.HealthCheckInterval = 10 * time.Second
	}
	if o.MaxSyncDistance == 0 {
		o.MaxSyncDistance = 4
	}
}

// Engine performs gated, sequential rollouts via an Actuator, gating on beacon
// health between nodes and aborting on the first node that fails to recover.
type Engine struct {
	actuator Actuator
	health   *BeaconHealth
	dora     *DoraHealth
	log      logrus.FieldLogger
}

// NewEngine returns an Engine.
func NewEngine(actuator Actuator, log logrus.FieldLogger) *Engine {
	if log == nil {
		log = logrus.New()
	}

	return &Engine{actuator: actuator, health: NewBeaconHealth(), log: log}
}

// checkHealth gates on Dora when a Dora URL is configured (the preferred,
// unauthenticated source of truth), otherwise falls back to the node's beacon.
func (e *Engine) checkHealth(ctx context.Context, target Target, maxSyncDistance uint64) (bool, string, error) {
	if e.dora != nil {
		return e.dora.Healthy(ctx, target.Name)
	}

	if target.BeaconURL == "" {
		return false, "no health source (set server doraURL or node beaconUrl)", nil
	}

	return e.health.Healthy(ctx, target.BeaconURL, maxSyncDistance)
}

// Run rolls the targets in order. It aborts on the first node that fails to
// recover, leaving the remaining targets untouched.
func (e *Engine) Run(ctx context.Context, targets []Target, opts Options) error {
	opts.applyDefaults()
	e.health.SetBasicAuth(opts.BeaconBasicAuthUser, opts.BeaconBasicAuthPass)

	if opts.DoraURL != "" {
		e.dora = NewDoraHealth(opts.DoraURL)
	}

	if len(targets) == 0 {
		return errors.New("no targets selected")
	}

	e.log.WithFields(logrus.Fields{
		"targets":  len(targets),
		"actuator": e.actuator.Name(),
		"image":    opts.Image,
		"dry_run":  opts.DryRun,
	}).Info("roll: starting")

	if !opts.SkipHealth {
		if err := e.preflight(ctx, targets, opts); err != nil {
			return fmt.Errorf("pre-flight: %w", err)
		}
	}

	for i, target := range targets {
		if err := ctx.Err(); err != nil {
			return err
		}

		entry := e.log.WithFields(logrus.Fields{
			"node": target.Name,
			"step": fmt.Sprintf("%d/%d", i+1, len(targets)),
		})

		progress := Progress{Node: target.Name, Index: i + 1, Total: len(targets)}

		if opts.DryRun {
			entry.Info("roll: dry-run, would trigger update")
			e.emit(opts, progress, PhaseSkipped, "dry-run: would trigger update")

			continue
		}

		e.emit(opts, progress, PhaseTriggering, "")

		if err := e.rollOne(ctx, entry, target, opts); err != nil {
			entry.WithError(err).Error("roll: aborted")
			e.emit(opts, progress, PhaseFailed, err.Error())
			e.reportRemaining(targets[i:])

			return fmt.Errorf("node %q: %w", target.Name, err)
		}

		e.emit(opts, progress, PhaseHealthy, "recovered")

		if i < len(targets)-1 {
			entry.WithField("delay", opts.DelayBetweenNodes).Info("roll: node healthy, waiting before next")

			if err := sleep(ctx, opts.DelayBetweenNodes); err != nil {
				return err
			}
		}
	}

	e.emit(opts, Progress{Total: len(targets)}, PhaseDone, "all targets updated and healthy")
	e.log.Info("roll: complete, all targets updated and healthy")

	return nil
}

// emit delivers a progress milestone to Options.OnProgress, if set.
func (e *Engine) emit(opts Options, p Progress, phase Phase, msg string) {
	if opts.OnProgress == nil {
		return
	}

	p.Phase = phase
	if msg != "" {
		p.Message = msg
	}

	opts.OnProgress(p)
}

func (e *Engine) rollOne(ctx context.Context, entry logrus.FieldLogger, target Target, opts Options) error {
	if !opts.SkipHealth {
		ok, reason, err := e.checkHealth(ctx, target, opts.MaxSyncDistance)
		if err != nil {
			return fmt.Errorf("pre-update health check: %w", err)
		}

		if !ok {
			return fmt.Errorf("not healthy before update: %s", reason)
		}
	}

	entry.Info("roll: triggering update")

	if err := e.actuator.Roll(ctx, target, opts.Image); err != nil {
		return fmt.Errorf("trigger: %w", err)
	}

	if err := sleep(ctx, opts.PostTriggerWait); err != nil {
		return err
	}

	if opts.SkipHealth {
		entry.Warn("roll: skipping recovery health check")

		return nil
	}

	entry.Info("roll: waiting for recovery")

	return e.waitHealthy(ctx, entry, target, opts)
}

func (e *Engine) waitHealthy(ctx context.Context, entry logrus.FieldLogger, target Target, opts Options) error {
	deadline := time.Now().Add(opts.WaitTimeout)
	ticker := time.NewTicker(opts.HealthCheckInterval)
	defer ticker.Stop()

	for {
		ok, reason, err := e.checkHealth(ctx, target, opts.MaxSyncDistance)
		switch {
		case err != nil:
			entry.WithError(err).Debug("roll: health check failed, retrying")
		case ok:
			entry.Info("roll: recovered")

			return nil
		default:
			entry.WithField("status", reason).Debug("roll: not yet healthy")
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("did not recover within %s", opts.WaitTimeout)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (e *Engine) preflight(ctx context.Context, targets []Target, opts Options) error {
	var unhealthy []string

	for _, target := range targets {
		ok, reason, err := e.checkHealth(ctx, target, opts.MaxSyncDistance)
		entry := e.log.WithField("node", target.Name)

		switch {
		case err != nil:
			entry.WithError(err).Warn("roll: pre-flight health error")
			unhealthy = append(unhealthy, target.Name)
		case !ok:
			entry.WithField("status", reason).Warn("roll: pre-flight not healthy")
			unhealthy = append(unhealthy, target.Name)
		default:
			entry.WithField("status", reason).Info("roll: pre-flight healthy")
		}
	}

	if len(unhealthy) > 0 {
		return fmt.Errorf("%d node(s) not healthy: %v", len(unhealthy), unhealthy)
	}

	return nil
}

func (e *Engine) reportRemaining(remaining []Target) {
	if len(remaining) <= 1 {
		return
	}

	names := make([]string, 0, len(remaining)-1)
	for _, t := range remaining[1:] {
		names = append(names, t.Name)
	}

	e.log.WithField("nodes", names).Warn("roll: the following nodes were NOT updated")
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
