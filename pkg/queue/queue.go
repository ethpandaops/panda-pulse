package queue

import (
	"context"
	"sync"
	"time"

	"github.com/ethpandaops/panda-pulse/pkg/store"
	"github.com/sirupsen/logrus"
)

// Queuer defines the interface for queue operations.
type Queuer interface {
	Start(ctx context.Context)
	Stop(ctx context.Context)
}

// AlertQueue is a concrete queue type for MonitorAlerts.
type AlertQueue struct {
	*Queue[*store.MonitorAlert]
}

// NewAlertQueue creates a new alert queue.
func NewAlertQueue(log *logrus.Logger, worker func(context.Context, *store.MonitorAlert) (bool, error), metrics *Metrics) *AlertQueue {
	return &AlertQueue{
		Queue: NewQueue[*store.MonitorAlert](log, worker, metrics),
	}
}

// Queue is a generic queue for processing items.
type Queue[T any] struct {
	log        *logrus.Logger
	queue      chan T
	processing sync.Map
	worker     func(context.Context, T) (bool, error)
	metrics    *Metrics
}

// NewQueue creates a new queue.
func NewQueue[T any](log *logrus.Logger, worker func(context.Context, T) (bool, error), metrics *Metrics) *Queue[T] {
	return &Queue[T]{
		log:     log,
		queue:   make(chan T, 100),
		worker:  worker,
		metrics: metrics,
	}
}

// SetWorker sets the worker function for processing items.
func (q *Queue[T]) SetWorker(worker func(context.Context, T) (bool, error)) {
	q.worker = worker
}

func (q *Queue[T]) Start(ctx context.Context) {
	go q.processQueue(ctx)
}

// Stop stops the queue processor.
func (q *Queue[T]) Stop(ctx context.Context) {
	// The queue processor will stop when the context is cancelled.
	q.metrics.queueLength.Set(0)
}

func (q *Queue[T]) Enqueue(item T) {
	if _, exists := q.processing.LoadOrStore(q.getItemKey(item), true); exists {
		q.metrics.skipsDueToLock.WithLabelValues(q.getItemNetwork(item), q.getItemClient(item)).Inc()
		q.log.WithFields(logrus.Fields{
			"network": q.getItemNetwork(item),
			"client":  q.getItemClient(item),
		}).Debug("Item already in progress, skipping")

		return
	}

	q.metrics.queuedTotal.WithLabelValues(q.getItemNetwork(item), q.getItemClient(item)).Inc()
	q.metrics.queueLength.Inc()
	q.queue <- item
}

// processQueue processes the queue of items.
func (q *Queue[T]) processQueue(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case item := <-q.queue:
			start := time.Now()
			key := q.getItemKey(item)

			q.metrics.queueLength.Dec()

			success, err := q.worker(ctx, item)
			duration := time.Since(start).Seconds()

			q.metrics.processingTime.WithLabelValues(q.getItemNetwork(item), q.getItemClient(item)).Observe(duration)

			if err != nil {
				q.metrics.failuresTotal.WithLabelValues(q.getItemNetwork(item), q.getItemClient(item), "worker_error").Inc()
				q.log.WithError(err).Error("Failed to process item")
			}

			status := "success"
			if !success {
				status = "failed"
			}

			q.metrics.processedTotal.WithLabelValues(q.getItemNetwork(item), q.getItemClient(item), status).Inc()

			q.processing.Delete(key)

			time.Sleep(1 * time.Second)
		}
	}
}

// getItemKey returns a unique key for the item.
func (q *Queue[T]) getItemKey(item T) string {
	return q.getItemNetwork(item) + "-" + q.getItemClient(item)
}

// getItemNetwork returns the network for the item.
func (q *Queue[T]) getItemNetwork(item T) string {
	if alert, ok := any(item).(*store.MonitorAlert); ok {
		return alert.Network
	}

	return "unknown"
}

// getItemClient returns the client for the item.
func (q *Queue[T]) getItemClient(item T) string {
	if alert, ok := any(item).(*store.MonitorAlert); ok {
		return alert.Client
	}

	return "unknown"
}
