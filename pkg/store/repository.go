package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/sirupsen/logrus"
)

var (
	DefaultRegion       = "us-east-1"
	DefaultBucketPrefix = "ethrand"
)

// Repository defines a generic interface for S3-backed storage.
type Repository[T any] interface {
	// List returns all items of type T.
	List(ctx context.Context) ([]T, error)
	// Persist stores an item of type T.
	Persist(ctx context.Context, item T) error
	// Purge removes an item based on its identifiers.
	Purge(ctx context.Context, identifiers ...string) error
	// Key returns the key for an item based on its identifiers.
	Key(item T) string
}

// BaseRepo contains common S3 functionality for all repositories.
type BaseRepo struct {
	store   *s3.Client
	bucket  string
	prefix  string
	log     *logrus.Logger
	metrics *Metrics
}

// S3Config contains the configuration for the S3 client.
type S3Config struct {
	AccessKeyID     string
	SecretAccessKey string
	Bucket          string
	Prefix          string
	EndpointURL     string // Optional. If empty, uses default SDK endpoints.
	Region          string // Optional. Defaults to us-east-1.
}

// NewBaseRepo creates a new base repository with common S3 functionality.
func NewBaseRepo(ctx context.Context, log *logrus.Logger, cfg *S3Config, metrics *Metrics) (BaseRepo, error) {
	opts := []func(*config.LoadOptions) error{
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AccessKeyID,
			cfg.SecretAccessKey,
			"",
		)),
		config.WithRegion(cfg.Region),
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return BaseRepo{}, fmt.Errorf("failed to load AWS config: %w", err)
	}

	cfgOpts := []func(*s3.Options){}

	if cfg.EndpointURL != "" {
		cfgOpts = append(cfgOpts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.EndpointURL)
			o.UsePathStyle = true
		})
	}

	return BaseRepo{
		store:   s3.NewFromConfig(awsCfg, cfgOpts...),
		bucket:  cfg.Bucket,
		prefix:  cfg.Prefix,
		log:     log,
		metrics: metrics,
	}, nil
}

// VerifyConnection verifies the S3 connection and bucket accessibility.
func (b *BaseRepo) VerifyConnection(ctx context.Context) error {
	// Test bucket listing.
	if _, err := b.store.ListBuckets(ctx, &s3.ListBucketsInput{}); err != nil {
		return fmt.Errorf("failed to list buckets: %w", err)
	}

	// Test bucket access.
	if _, err := b.store.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(b.bucket),
	}); err != nil {
		return fmt.Errorf("failed to access bucket %s: %w", b.bucket, err)
	}

	b.log.WithFields(logrus.Fields{
		"bucket": b.bucket,
		"prefix": b.prefix,
	}).Info("Verified S3 connection")

	return nil
}

// GetS3Client returns the underlying S3 client.
func (b *BaseRepo) GetS3Client() *s3.Client {
	return b.store
}

// observeOperation observes the operation and increments the metrics.
func (b *BaseRepo) observeOperation(operation, repository string, err error) {
	b.metrics.operationsTotal.WithLabelValues(operation, repository).Inc()

	if err != nil {
		errType := "unknown"

		if strings.Contains(err.Error(), "context deadline exceeded") {
			errType = "timeout"
		} else if strings.Contains(err.Error(), "not found") {
			errType = "not_found"
		}

		b.metrics.operationErrors.WithLabelValues(operation, repository, errType).Inc()
	}
}

// trackDuration tracks the duration of an operation and observes the metrics.
func (b *BaseRepo) trackDuration(operation, repository string) func() {
	start := time.Now()

	return func() {
		b.metrics.operationDuration.WithLabelValues(operation, repository).Observe(time.Since(start).Seconds())
	}
}
