package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/ethpandaops/panda-pulse/pkg/clients"
	"github.com/sirupsen/logrus"
)

type MonitorRepo struct {
	store  *s3.Client
	bucket string
	prefix string
	log    *logrus.Logger
}

type MonitorAlert struct {
	Network        string             `json:"network"`
	DiscordChannel string             `json:"discord_channel"`
	Client         string             `json:"client"`
	ClientType     clients.ClientType `json:"client_type"`
	CreatedAt      time.Time          `json:"created_at"`
	UpdatedAt      time.Time          `json:"updated_at"`
}

func NewMonitorRepo(ctx context.Context, log *logrus.Logger, cfg *S3Config) (*MonitorRepo, error) {
	awsCfg, err := config.LoadDefaultConfig(
		ctx,
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AccessKeyID,
			cfg.SecretAccessKey,
			"",
		)),
		config.WithEndpointResolver(aws.EndpointResolverFunc(
			func(service, region string) (aws.Endpoint, error) {
				return aws.Endpoint{
					URL:               "http://localhost:4566",
					SigningRegion:     "us-east-1",
					HostnameImmutable: true,
				}, nil
			},
		)),
		config.WithRegion("us-east-1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &MonitorRepo{
		store:  s3.NewFromConfig(awsCfg),
		bucket: cfg.Bucket,
		prefix: cfg.Prefix,
		log:    log,
	}, nil
}

func (s *MonitorRepo) ListMonitorAlerts(ctx context.Context) ([]*MonitorAlert, error) {
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
			if !strings.HasSuffix(*obj.Key, ".json") || !strings.Contains(*obj.Key, "/alerts/") {
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

func (s *MonitorRepo) RegisterMonitorAlert(ctx context.Context, alert *MonitorAlert) error {
	data, err := json.Marshal(alert)
	if err != nil {
		return fmt.Errorf("failed to marshal alert: %w", err)
	}

	fmt.Printf("Registering alert for network=%s channel=%s bucket=%s\n", alert.Network, alert.DiscordChannel, s.bucket)

	key := s.alertKey(alert.Network, alert.Client)
	_, err = s.store.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	})
	if err != nil {
		return fmt.Errorf("failed to put alert: %w", err)
	}

	return nil
}

func (s *MonitorRepo) DeleteMonitorAlert(ctx context.Context, network, client string) error {
	key := s.alertKey(network, client)

	_, err := s.store.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("failed to delete alert: %w", err)
	}

	return nil
}

func (s *MonitorRepo) alertKey(network, client string) string {
	return fmt.Sprintf("%s/networks/%s/alerts/%s.json", s.prefix, network, client)
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
