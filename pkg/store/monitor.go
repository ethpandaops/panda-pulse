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
	CheckID        string             `json:"check_id"`
	Network        string             `json:"network"`
	DiscordChannel string             `json:"discord_channel"`
	Client         string             `json:"client"`
	ClientType     clients.ClientType `json:"client_type"`
	CreatedAt      time.Time          `json:"created_at"`
	UpdatedAt      time.Time          `json:"updated_at"`
}

// NewMonitorRepo creates a new MonitorRepo.
func NewMonitorRepo(ctx context.Context, log *logrus.Logger, cfg *S3Config) (*MonitorRepo, error) {
	baseRepo, err := NewBaseRepo(ctx, log, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create base repo: %w", err)
	}

	return &MonitorRepo{
		BaseRepo: baseRepo,
	}, nil
}

// List implements Repository[*MonitorAlert].
func (s *MonitorRepo) List(ctx context.Context) ([]*MonitorAlert, error) {
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(fmt.Sprintf("%s/networks/", s.prefix)),
	}

	var alerts []*MonitorAlert
	paginator := s3.NewListObjectsV2Paginator(s.store, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
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

	return alerts, nil
}

// Persist implements Repository[*MonitorAlert].
func (s *MonitorRepo) Persist(ctx context.Context, alert *MonitorAlert) error {
	data, err := json.Marshal(alert)
	if err != nil {
		return fmt.Errorf("failed to marshal alert: %w", err)
	}

	if _, err = s.store.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.Key(alert)),
		Body:   bytes.NewReader(data),
	}); err != nil {
		return fmt.Errorf("failed to put alert: %w", err)
	}

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
