package queue

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethpandaops/panda-pulse/pkg/store"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestQueue(t *testing.T) {
	t.Run("processes items in order", func(t *testing.T) {
		var processed int32
		worker := func(ctx context.Context, alert *store.MonitorAlert) (bool, error) {
			atomic.AddInt32(&processed, 1)

			return true, nil
		}

		q := NewQueue(logrus.New(), worker)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		q.Start(ctx)

		alerts := []*store.MonitorAlert{
			{Network: "net1", Client: "client1"},
			{Network: "net1", Client: "client2"},
			{Network: "net2", Client: "client1"},
		}

		for _, alert := range alerts {
			q.Enqueue(alert)
		}

		// Wait for processing.
		time.Sleep(7 * time.Second) // 2s delay * 3 items + buffer.
		assert.Equal(t, int32(3), atomic.LoadInt32(&processed))
	})

	t.Run("prevents duplicate processing", func(t *testing.T) {
		var processed int32
		worker := func(ctx context.Context, alert *store.MonitorAlert) (bool, error) {
			atomic.AddInt32(&processed, 1)

			return true, nil
		}

		q := NewQueue(logrus.New(), worker)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		q.Start(ctx)

		alert := &store.MonitorAlert{Network: "net1", Client: "client1"}
		q.Enqueue(alert)
		q.Enqueue(alert) // Duplicate.

		time.Sleep(3 * time.Second)
		assert.Equal(t, int32(1), atomic.LoadInt32(&processed))
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		var processed int32

		worker := func(ctx context.Context, alert *store.MonitorAlert) (bool, error) {
			atomic.AddInt32(&processed, 1)

			return true, nil
		}

		q := NewQueue(logrus.New(), worker)
		ctx, cancel := context.WithCancel(context.Background())
		q.Start(ctx)

		// Cancel before enqueueing.
		cancel()
		time.Sleep(100 * time.Millisecond)

		q.Enqueue(&store.MonitorAlert{Network: "net1", Client: "client1"})
		time.Sleep(3 * time.Second)
		assert.Equal(t, int32(0), atomic.LoadInt32(&processed))
	})
}

func TestGetAlertKey(t *testing.T) {
	q := NewQueue(logrus.New(), nil)
	alert := &store.MonitorAlert{
		Network: "testnet",
		Client:  "client1",
	}
	assert.Equal(t, "testnet-client1", q.getAlertKey(alert))
}
