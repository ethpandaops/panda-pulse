package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/ethpandaops/panda-pulse/pkg/checks"
)

type NetworkAlert struct {
	Network        string            `json:"network"`
	Client         string            `json:"client"`      // The specific client name
	ClientType     checks.ClientType `json:"client_type"` // ClientTypeCL or ClientTypeEL
	DiscordChannel string            `json:"discord_channel"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

type S3Config struct {
	AccessKeyID     string
	SecretAccessKey string
	Bucket          string
	Prefix          string
}

type S3Store struct {
	client *s3.Client
	bucket string
	prefix string
}

func NewS3Store(cfg *S3Config) (*S3Store, error) {
	awsCfg, err := config.LoadDefaultConfig(context.Background(),
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

	client := s3.NewFromConfig(awsCfg)
	return &S3Store{
		client: client,
		bucket: cfg.Bucket,
		prefix: cfg.Prefix,
	}, nil
}

func (s *S3Store) alertKey(network, client string) string {
	return fmt.Sprintf("%s/networks/%s/alerts/%s.json", s.prefix, network, client)
}

func (s *S3Store) ListNetworkAlerts(ctx context.Context) ([]*NetworkAlert, error) {
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(fmt.Sprintf("%s/networks/", s.prefix)),
	}

	var alerts []*NetworkAlert
	paginator := s3.NewListObjectsV2Paginator(s.client, input)
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
				log.Printf("Failed to get alert %s: %v", *obj.Key, err)
				continue
			}
			alerts = append(alerts, alert)
		}
	}

	return alerts, nil
}

func (s *S3Store) RegisterNetworkAlert(ctx context.Context, alert *NetworkAlert) error {
	data, err := json.Marshal(alert)
	if err != nil {
		return fmt.Errorf("failed to marshal alert: %w", err)
	}

	fmt.Printf("Registering alert for network=%s channel=%s bucket=%s\n", alert.Network, alert.DiscordChannel, s.bucket)

	key := s.alertKey(alert.Network, alert.Client)
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	})
	if err != nil {
		return fmt.Errorf("failed to put alert: %w", err)
	}

	return nil
}

func (s *S3Store) getAlert(ctx context.Context, key string) (*NetworkAlert, error) {
	output, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get alert: %w", err)
	}
	defer output.Body.Close()

	var alert NetworkAlert
	if err := json.NewDecoder(output.Body).Decode(&alert); err != nil {
		return nil, fmt.Errorf("failed to decode alert: %w", err)
	}

	return &alert, nil
}

func (s *S3Store) DeleteNetworkAlert(ctx context.Context, network, client string) error {
	key := s.alertKey(network, client)

	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("failed to delete alert: %w", err)
	}

	return nil
}
