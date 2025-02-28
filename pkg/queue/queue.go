package queue

import (
	"context"
	"sync"
	"time"

	"github.com/ethpandaops/panda-pulse/pkg/store"
	"github.com/sirupsen/logrus"
)

// Queue is a queue of alerts to be processed.
type Queue struct {
	log        *logrus.Logger
	queue      chan *store.MonitorAlert
	processing sync.Map
	worker     func(context.Context, *store.MonitorAlert) (bool, error)
	metrics    *Metrics
}

// NewQueue creates a new check queue.
func NewQueue(log *logrus.Logger, worker func(context.Context, *store.MonitorAlert) (bool, error), metrics *Metrics) *Queue {
	return &Queue{
		log:     log,
		queue:   make(chan *store.MonitorAlert, 100),
		worker:  worker,
		metrics: metrics,
	}
}

// SetWorker sets the worker function for processing alerts.
func (q *Queue) SetWorker(worker func(context.Context, *store.MonitorAlert) (bool, error)) {
	q.worker = worker
}

func (q *Queue) Start(ctx context.Context) {
	go q.processQueue(ctx)
}

func (q *Queue) Enqueue(alert *store.MonitorAlert) {
	if _, exists := q.processing.LoadOrStore(q.getAlertKey(alert), true); exists {
		q.metrics.skipsDueToLock.WithLabelValues(alert.Network, alert.Client).Inc()
		q.log.WithFields(logrus.Fields{
			"network": alert.Network,
			"client":  alert.Client,
		}).Debug("Check already in progress, skipping")

		return
	}

	q.metrics.queuedTotal.WithLabelValues(alert.Network, alert.Client).Inc()
	q.metrics.queueLength.Inc()
	q.queue <- alert
}

// processQueue processes the queue of alerts.
func (q *Queue) processQueue(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case alert := <-q.queue:
			start := time.Now()
			key := q.getAlertKey(alert)

			q.metrics.queueLength.Dec()

			success, err := q.worker(ctx, alert)
			duration := time.Since(start).Seconds()

			q.metrics.processingTime.WithLabelValues(alert.Network, alert.Client).Observe(duration)

			if err != nil {
				q.metrics.failuresTotal.WithLabelValues(alert.Network, alert.Client, "worker_error").Inc()
				q.log.WithError(err).Error("Failed to process check")
			}

			status := "success"
			if !success {
				status = "failed"
			}

			q.metrics.processedTotal.WithLabelValues(alert.Network, alert.Client, status).Inc()

			q.processing.Delete(key)

			time.Sleep(1 * time.Second)
		}
	}
}

// getAlertKey returns a unique key for the alert.
func (q *Queue) getAlertKey(alert *store.MonitorAlert) string {
	return alert.Network + "-" + alert.Client
}
