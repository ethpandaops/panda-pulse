package scheduler

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/robfig/cron/v3"
)

type Job struct {
	Name     string
	Schedule string
	Run      func(context.Context) error
}

type Scheduler struct {
	cron *cron.Cron
	jobs map[string]cron.EntryID // Track jobs by name
	mu   sync.Mutex
}

func NewScheduler() *Scheduler {
	return &Scheduler{
		cron: cron.New(),
		jobs: make(map[string]cron.EntryID),
	}
}

func (s *Scheduler) AddJob(name, schedule string, run func(context.Context) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// If job already exists, remove it first
	if id, exists := s.jobs[name]; exists {
		s.cron.Remove(id)
	}

	id, err := s.cron.AddFunc(schedule, func() {
		ctx := context.Background()
		if err := run(ctx); err != nil {
			log.Printf("job %s failed: %v", name, err)
		}
	})
	if err != nil {
		return fmt.Errorf("failed to add job %s: %w", name, err)
	}

	s.jobs[name] = id
	return nil
}

func (s *Scheduler) RemoveJob(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if id, exists := s.jobs[name]; exists {
		s.cron.Remove(id)
		delete(s.jobs, name)
	}
}

func (s *Scheduler) Start() {
	s.cron.Start()
}

func (s *Scheduler) Stop() {
	s.cron.Stop()
}
