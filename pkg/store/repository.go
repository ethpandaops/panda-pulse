package store

import (
	"context"
	"fmt"

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
}

// BaseRepo contains common S3 functionality for all repositories.
type BaseRepo struct {
	store  *s3.Client
	bucket string
	prefix string
	log    *logrus.Logger
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
func NewBaseRepo(ctx context.Context, log *logrus.Logger, cfg *S3Config) (BaseRepo, error) {
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
		store:  s3.NewFromConfig(awsCfg, cfgOpts...),
		bucket: cfg.Bucket,
		prefix: cfg.Prefix,
		log:    log,
	}, nil
}

// GetS3Client returns the underlying S3 client.
func (b *BaseRepo) GetS3Client() *s3.Client {
	return b.store
}
