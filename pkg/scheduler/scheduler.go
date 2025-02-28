package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/sirupsen/logrus"
)

type Job struct {
	Name     string
	Schedule string
	Run      func(context.Context) error
}

type Scheduler struct {
	log     *logrus.Logger
	cron    *cron.Cron
	jobs    map[string]cron.EntryID // Track jobs by name
	mu      sync.Mutex
	metrics *Metrics
}

func NewScheduler(log *logrus.Logger, metrics *Metrics) *Scheduler {
	return &Scheduler{
		log:     log,
		cron:    cron.New(),
		jobs:    make(map[string]cron.EntryID),
		metrics: metrics,
	}
}

func (s *Scheduler) AddJob(name, schedule string, run func(context.Context) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if id, exists := s.jobs[name]; exists {
		s.cron.Remove(id)
		s.metrics.activeJobs.Dec()
	}

	id, err := s.cron.AddFunc(schedule, func() {
		ctx := context.Background()
		start := time.Now()

		s.metrics.jobExecutions.WithLabelValues(name, schedule).Inc()
		s.metrics.lastExecutionTS.WithLabelValues(name, schedule).Set(float64(time.Now().Unix()))

		if err := run(ctx); err != nil {
			s.metrics.jobFailures.WithLabelValues(name, schedule).Inc()
			s.log.Errorf("job %s failed: %v", name, err)
		}

		s.metrics.executionTime.WithLabelValues(name).Observe(time.Since(start).Seconds())
	})

	if err != nil {
		return fmt.Errorf("failed to add job %s: %w", name, err)
	}

	s.jobs[name] = id
	s.metrics.jobsTotal.WithLabelValues(schedule).Inc()
	s.metrics.activeJobs.Inc()

	return nil
}

func (s *Scheduler) RemoveJob(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if id, exists := s.jobs[name]; exists {
		s.cron.Remove(id)
		delete(s.jobs, name)
		s.metrics.activeJobs.Dec()
	}
}

func (s *Scheduler) Start() {
	s.cron.Start()
}

func (s *Scheduler) Stop() {
	s.cron.Stop()
}
