package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/ethpandaops/panda-pulse/pkg/clients"
	"github.com/sirupsen/logrus"
)

// MonitorRepo implements Repository[*MonitorAlert].
type MonitorRepo struct {
	BaseRepo
}

// MonitorAlert represents a monitor alert.
type MonitorAlert struct {
	Network        string             `json:"network"`
	Client         string             `json:"client"`
	CheckID        string             `json:"checkId"`
	Enabled        bool               `json:"enabled"`
	DiscordChannel string             `json:"discordChannel"`
	DiscordGuildID string             `json:"discordGuildId"`
	Interval       time.Duration      `json:"interval"`
	Schedule       string             `json:"schedule"`
	ClientType     clients.ClientType `json:"clientType"`
	CreatedAt      time.Time          `json:"createdAt"`
	UpdatedAt      time.Time          `json:"updatedAt"`
}

// NewMonitorRepo creates a new MonitorRepo.
func NewMonitorRepo(ctx context.Context, log *logrus.Logger, cfg *S3Config, metrics *Metrics) (*MonitorRepo, error) {
	baseRepo, err := NewBaseRepo(ctx, log, cfg, metrics)
	if err != nil {
		return nil, fmt.Errorf("failed to create base repo: %w", err)
	}

	return &MonitorRepo{
		BaseRepo: baseRepo,
	}, nil
}

// List implements Repository[*MonitorAlert].
func (s *MonitorRepo) List(ctx context.Context) ([]*MonitorAlert, error) {
	defer s.trackDuration("list", "monitor")()

	var (
		input = &s3.ListObjectsV2Input{
			Bucket: aws.String(s.bucket),
			Prefix: aws.String(fmt.Sprintf("%s/networks/", s.prefix)),
		}
		alerts    []*MonitorAlert
		paginator = s3.NewListObjectsV2Paginator(s.store, input)
	)

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			s.observeOperation("list", "monitor", err)

			return nil, fmt.Errorf("failed to list alerts: %w", err)
		}

		for _, obj := range page.Contents {
			if !strings.HasSuffix(*obj.Key, ".json") || !strings.Contains(*obj.Key, "/monitor/") {
				continue
			}

			alert, err := s.getAlert(ctx, *obj.Key)
			if err != nil {
				s.log.Errorf("Failed to get alert %s: %v", *obj.Key, err)

				continue
			}

			alerts = append(alerts, alert)
		}
	}

	s.metrics.objectsTotal.WithLabelValues("monitor").Set(float64(len(alerts)))

	return alerts, nil
}

// Persist implements Repository[*MonitorAlert].
func (s *MonitorRepo) Persist(ctx context.Context, alert *MonitorAlert) error {
	defer s.trackDuration("persist", "monitor")()

	data, err := json.Marshal(alert)
	if err != nil {
		s.observeOperation("persist", "monitor", err)

		return fmt.Errorf("failed to marshal alert: %w", err)
	}

	s.metrics.objectSizeBytes.WithLabelValues("monitor").Observe(float64(len(data)))

	if _, err = s.store.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.Key(alert)),
		Body:   bytes.NewReader(data),
	}); err != nil {
		s.observeOperation("persist", "monitor", err)

		return fmt.Errorf("failed to put alert: %w", err)
	}

	s.observeOperation("persist", "monitor", nil)

	return nil
}

// Purge implements Repository[*MonitorAlert].
func (s *MonitorRepo) Purge(ctx context.Context, identifiers ...string) error {
	if len(identifiers) != 2 {
		return fmt.Errorf("expected network and client identifiers, got %d identifiers", len(identifiers))
	}

	network, client := identifiers[0], identifiers[1]

	if _, err := s.store.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.Key(&MonitorAlert{Network: network, Client: client})),
	}); err != nil {
		return fmt.Errorf("failed to delete alert: %w", err)
	}

	return nil
}

// Key implements Repository[*MonitorAlert].
func (s *MonitorRepo) Key(alert *MonitorAlert) string {
	if alert == nil {
		s.log.Error("alert is nil")

		return ""
	}

	return fmt.Sprintf("%s/networks/%s/monitor/%s.json", s.prefix, alert.Network, alert.Client)
}

func (s *MonitorRepo) getAlert(ctx context.Context, key string) (*MonitorAlert, error) {
	output, err := s.store.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get alert: %w", err)
	}

	defer output.Body.Close()

	var alert MonitorAlert
	if err := json.NewDecoder(output.Body).Decode(&alert); err != nil {
		return nil, fmt.Errorf("failed to decode alert: %w", err)
	}

	return &alert, nil
}
