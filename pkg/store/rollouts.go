package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/sirupsen/logrus"
)

// RolloutAlert sends a network's rollout events to a channel. LastEventID
// is the newest rolloor event already handled.
type RolloutAlert struct {
	Network        string    `json:"network"`
	DiscordGuildID string    `json:"discordGuildId"`
	DiscordChannel string    `json:"discordChannel"`
	LastEventID    int64     `json:"lastEventId"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// RolloutsRepo implements Repository[*RolloutAlert].
type RolloutsRepo struct {
	BaseRepo
}

// NewRolloutsRepo creates a new RolloutsRepo.
func NewRolloutsRepo(ctx context.Context, log *logrus.Logger, cfg *S3Config, metrics *Metrics) (*RolloutsRepo, error) {
	baseRepo, err := NewBaseRepo(ctx, log, cfg, metrics)
	if err != nil {
		return nil, fmt.Errorf("failed to create base repo: %w", err)
	}

	return &RolloutsRepo{BaseRepo: baseRepo}, nil
}

// List implements Repository[*RolloutAlert].
func (s *RolloutsRepo) List(ctx context.Context) ([]*RolloutAlert, error) {
	defer s.trackDuration("list", "rollouts")()

	var (
		input = &s3.ListObjectsV2Input{
			Bucket: aws.String(s.bucket),
			Prefix: aws.String(fmt.Sprintf("%s/networks/", s.prefix)),
		}
		alerts    []*RolloutAlert
		paginator = s3.NewListObjectsV2Paginator(s.store, input)
	)

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			s.observeOperation("list", "rollouts", err)

			return nil, fmt.Errorf("failed to list rollout alerts: %w", err)
		}

		for _, obj := range page.Contents {
			if !strings.HasSuffix(*obj.Key, ".json") || !strings.Contains(*obj.Key, "/rollouts/") {
				continue
			}

			alert, err := s.getAlert(ctx, *obj.Key)
			if err != nil {
				continue
			}

			alerts = append(alerts, alert)
		}
	}

	s.observeOperation("list", "rollouts", nil)
	s.metrics.objectsTotal.WithLabelValues("rollouts").Set(float64(len(alerts)))

	return alerts, nil
}

// Get returns a network's alert in a guild, or ErrRolloutAlertNotFound.
func (s *RolloutsRepo) Get(ctx context.Context, network, guildID string) (*RolloutAlert, error) {
	defer s.trackDuration("get", "rollouts")()

	alert, err := s.getAlert(ctx, s.Key(&RolloutAlert{Network: network, DiscordGuildID: guildID}))
	if err != nil {
		var noSuchKey *types.NoSuchKey
		if errors.As(err, &noSuchKey) {
			s.observeOperation("get", "rollouts", nil)

			return nil, ErrRolloutAlertNotFound
		}

		s.observeOperation("get", "rollouts", err)

		return nil, err
	}

	s.observeOperation("get", "rollouts", nil)

	return alert, nil
}

// Persist implements Repository[*RolloutAlert].
func (s *RolloutsRepo) Persist(ctx context.Context, alert *RolloutAlert) error {
	defer s.trackDuration("persist", "rollouts")()

	data, err := json.Marshal(alert)
	if err != nil {
		s.observeOperation("persist", "rollouts", err)

		return fmt.Errorf("failed to marshal rollout alert: %w", err)
	}

	s.metrics.objectSizeBytes.WithLabelValues("rollouts").Observe(float64(len(data)))

	if _, err = s.store.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.Key(alert)),
		Body:   bytes.NewReader(data),
	}); err != nil {
		s.observeOperation("persist", "rollouts", err)

		return fmt.Errorf("failed to put rollout alert: %w", err)
	}

	s.observeOperation("persist", "rollouts", nil)

	return nil
}

// Purge implements Repository[*RolloutAlert]; identifiers are network and
// guild ID.
func (s *RolloutsRepo) Purge(ctx context.Context, identifiers ...string) error {
	defer s.trackDuration("purge", "rollouts")()

	if len(identifiers) != 2 {
		return fmt.Errorf("expected network and guildID identifiers, got %d identifiers", len(identifiers))
	}

	if _, err := s.store.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.Key(&RolloutAlert{Network: identifiers[0], DiscordGuildID: identifiers[1]})),
	}); err != nil {
		s.observeOperation("purge", "rollouts", err)

		return fmt.Errorf("failed to delete rollout alert: %w", err)
	}

	s.observeOperation("purge", "rollouts", nil)

	return nil
}

// Key implements Repository[*RolloutAlert].
func (s *RolloutsRepo) Key(alert *RolloutAlert) string {
	if alert == nil {
		return ""
	}

	return fmt.Sprintf("%s/networks/%s/rollouts/%s.json", s.prefix, alert.Network, alert.DiscordGuildID)
}

func (s *RolloutsRepo) getAlert(ctx context.Context, key string) (*RolloutAlert, error) {
	output, err := s.store.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get rollout alert: %w", err)
	}

	defer output.Body.Close()

	var alert RolloutAlert
	if err := json.NewDecoder(output.Body).Decode(&alert); err != nil {
		return nil, fmt.Errorf("failed to decode rollout alert: %w", err)
	}

	return &alert, nil
}
