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
}

// NewQueue creates a new check queue.
func NewQueue(log *logrus.Logger, worker func(context.Context, *store.MonitorAlert) (bool, error)) *Queue {
	return &Queue{
		log:    log,
		queue:  make(chan *store.MonitorAlert, 100), // Buffer size of 100 - this is ample give number of clients <> devnets at anyone time.
		worker: worker,
	}
}

func (q *Queue) Start(ctx context.Context) {
	go q.processQueue(ctx)
}

func (q *Queue) Enqueue(alert *store.MonitorAlert) {
	// Don't queue if already processing
	if _, exists := q.processing.LoadOrStore(q.getAlertKey(alert), true); exists {
		q.log.WithFields(logrus.Fields{
			"network": alert.Network,
			"client":  alert.Client,
		}).Debug("Check already in progress, skipping")

		return
	}

	q.queue <- alert
}

// processQueue processes the queue of alerts.
func (q *Queue) processQueue(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case alert := <-q.queue:
			key := q.getAlertKey(alert)

			q.log.WithFields(logrus.Fields{
				"network": alert.Network,
				"client":  alert.Client,
			}).Info("Processing check from queue")

			if _, err := q.worker(ctx, alert); err != nil {
				q.log.WithError(err).Error("Failed to process check")
			}

			// Remove from processing map after completion..
			q.processing.Delete(key)

			// Add small artificial delay between checks.
			time.Sleep(2 * time.Second)
		}
	}
}

// getAlertKey returns a unique key for the alert.
func (q *Queue) getAlertKey(alert *store.MonitorAlert) string {
	return alert.Network + "-" + alert.Client
}
