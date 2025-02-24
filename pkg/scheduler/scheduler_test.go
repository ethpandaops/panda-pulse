package scheduler

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScheduler(t *testing.T) {
	t.Run("NewScheduler", func(t *testing.T) {
		log := logrus.New()
		s := NewScheduler(log)
		require.NotNil(t, s)
		require.NotNil(t, s.cron)
		require.NotNil(t, s.jobs)
	})

	t.Run("AddJob", func(t *testing.T) {
		s := NewScheduler(logrus.New())

		require.NoError(t, s.AddJob("test", "* * * * *", func(ctx context.Context) error {
			return nil
		}))

		// Verify job was added
		assert.Len(t, s.jobs, 1)
		_, exists := s.jobs["test"]
		assert.True(t, exists)
	})

	t.Run("AddJob_InvalidSchedule", func(t *testing.T) {
		s := NewScheduler(logrus.New())

		err := s.AddJob("test", "invalid", func(ctx context.Context) error {
			return nil
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to add job test")
	})

	t.Run("AddJob_Replaces", func(t *testing.T) {
		s := NewScheduler(logrus.New())

		// Add initial job.
		require.NoError(t, s.AddJob("test", "* * * * *", func(ctx context.Context) error {
			return nil
		}))
		firstID := s.jobs["test"]

		// Replace with new job.
		require.NoError(t, s.AddJob("test", "*/5 * * * *", func(ctx context.Context) error {
			return nil
		}))

		// Verify job was replaced.
		assert.Len(t, s.jobs, 1)
		assert.NotEqual(t, firstID, s.jobs["test"])
	})

	t.Run("RemoveJob", func(t *testing.T) {
		s := NewScheduler(logrus.New())

		require.NoError(t, s.AddJob("test", "* * * * *", func(ctx context.Context) error {
			return nil
		}))

		s.RemoveJob("test")
		assert.Len(t, s.jobs, 0)
	})

	t.Run("RemoveJob_NonExistent", func(t *testing.T) {
		s := NewScheduler(logrus.New())
		// Should not panic.
		s.RemoveJob("nonexistent")
	})

	t.Run("Job_Execution", func(t *testing.T) {
		s := NewScheduler(logrus.New())

		var wg sync.WaitGroup
		wg.Add(1)

		executed := false
		require.NoError(t, s.AddJob("test", "@every 10ms", func(ctx context.Context) error {
			executed = true
			wg.Done()

			return nil
		}))

		s.Start()
		defer s.Stop()

		// Wait for job execution or timeout.
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			assert.True(t, executed)
		case <-time.After(time.Second):
			t.Fatal("job did not execute within timeout")
		}
	})

	t.Run("Job_Error", func(t *testing.T) {
		var logBuf logrus.Logger
		log := &logBuf
		s := NewScheduler(log)

		var wg sync.WaitGroup
		wg.Add(1)

		require.NoError(t, s.AddJob("test", "@every 10ms", func(ctx context.Context) error {
			wg.Done()

			return assert.AnError
		}))

		s.Start()
		defer s.Stop()

		wg.Wait()
	})

	t.Run("Concurrent_Operations", func(t *testing.T) {
		s := NewScheduler(logrus.New())
		s.Start()
		defer s.Stop()

		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				name := fmt.Sprintf("job-%d", i)

				assert.NoError(t, s.AddJob(name, "* * * * *", func(ctx context.Context) error {
					return nil
				}))

				time.Sleep(time.Millisecond)
				s.RemoveJob(name)
			}(i)
		}

		wg.Wait()
	})
}
