package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/sirupsen/logrus"
)

// CheckArtifact represents a single artifact from a check run.
type CheckArtifact struct {
	Network   string    `json:"network"`
	Client    string    `json:"client"`
	CheckID   string    `json:"checkId"`
	Type      string    `json:"type"` // log, png, etc
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Content   []byte    `json:"content"`
}

// ChecksRepo implements Repository for check artifacts.
type ChecksRepo struct {
	BaseRepo
}

// NewChecksRepo creates a new ChecksRepo.
func NewChecksRepo(ctx context.Context, log *logrus.Logger, cfg *S3Config, metrics *Metrics) (*ChecksRepo, error) {
	baseRepo, err := NewBaseRepo(ctx, log, cfg, metrics)
	if err != nil {
		return nil, fmt.Errorf("failed to create base repo: %w", err)
	}

	return &ChecksRepo{
		BaseRepo: baseRepo,
	}, nil
}

// List implements Repository[*CheckArtifact].
func (s *ChecksRepo) List(ctx context.Context) ([]*CheckArtifact, error) {
	defer s.trackDuration("list", "checks")()

	var (
		artifacts []*CheckArtifact
		input     = &s3.ListObjectsV2Input{
			Bucket: aws.String(s.bucket),
			Prefix: aws.String(fmt.Sprintf("%s/networks/", s.prefix)),
		}
		paginator = s3.NewListObjectsV2Paginator(s.store, input)
	)

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			s.observeOperation("list", "checks", err)

			return nil, fmt.Errorf("failed to list artifacts: %w", err)
		}

		for _, obj := range page.Contents {
			if !strings.Contains(*obj.Key, "/checks/") {
				continue
			}

			// Extract checkID from the key
			// Format: prefix/networks/{network}/checks/{client}/{checkID}.{ext}
			parts := strings.Split(*obj.Key, "/")
			if len(parts) < 6 {
				continue
			}

			fileName := parts[len(parts)-1]
			checkID := strings.TrimSuffix(strings.TrimSuffix(fileName, ".log"), ".json")
			network := parts[len(parts)-4]
			client := parts[len(parts)-2]

			// Skip if we already have this artifact
			exists := false

			for _, a := range artifacts {
				if a.CheckID == checkID {
					exists = true

					break
				}
			}

			if exists {
				continue
			}

			// If it's a JSON file, try to parse it
			if strings.HasSuffix(*obj.Key, ".json") {
				artifact, err := s.getArtifact(ctx, *obj.Key)
				if err != nil {
					s.log.Errorf("Failed to get artifact %s: %v", *obj.Key, err)

					continue
				}

				artifacts = append(artifacts, artifact)

				continue
			}

			// If it's a log file, create an artifact from the path info
			if strings.HasSuffix(*obj.Key, ".log") {
				artifacts = append(artifacts, &CheckArtifact{
					Network:   network,
					Client:    client,
					CheckID:   checkID,
					Type:      "log",
					CreatedAt: *obj.LastModified,
					UpdatedAt: *obj.LastModified,
				})
			}
		}
	}

	s.metrics.objectsTotal.WithLabelValues("checks").Set(float64(len(artifacts)))

	return artifacts, nil
}

// Persist implements Repository[*CheckArtifact].
func (s *ChecksRepo) Persist(ctx context.Context, artifact *CheckArtifact) error {
	defer s.trackDuration("persist", "checks")()

	put := &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.Key(artifact)),
	}

	if len(artifact.Content) > 0 {
		contentType := http.DetectContentType(artifact.Content)

		put.Body = bytes.NewReader(artifact.Content)
		put.ContentType = aws.String(contentType)

		s.metrics.objectSizeBytes.WithLabelValues("checks").Observe(float64(len(artifact.Content)))
	}

	if _, err := s.store.PutObject(ctx, put); err != nil {
		s.observeOperation("persist", "checks", err)

		return fmt.Errorf("failed to put artifact: %w", err)
	}

	s.observeOperation("persist", "checks", nil)

	return nil
}

// Purge implements Repository[*CheckArtifact].
func (s *ChecksRepo) Purge(ctx context.Context, identifiers ...string) error {
	if len(identifiers) != 3 {
		return fmt.Errorf("expected network, client and checkID identifiers, got %d identifiers", len(identifiers))
	}

	var (
		network, client, checkID = identifiers[0], identifiers[1], identifiers[2]
		prefix                   = fmt.Sprintf("%s/networks/%s/checks/%s/%s", s.prefix, network, client, checkID)
		input                    = &s3.ListObjectsV2Input{
			Bucket: aws.String(s.bucket),
			Prefix: aws.String(prefix),
		}
		paginator = s3.NewListObjectsV2Paginator(s.store, input)
	)

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list objects for deletion: %w", err)
		}

		for _, obj := range page.Contents {
			if _, err := s.store.DeleteObject(ctx, &s3.DeleteObjectInput{
				Bucket: aws.String(s.bucket),
				Key:    obj.Key,
			}); err != nil {
				return fmt.Errorf("failed to delete object %s: %w", *obj.Key, err)
			}
		}
	}

	return nil
}

// Key implements Repository[*CheckArtifact].
func (s *ChecksRepo) Key(artifact *CheckArtifact) string {
	if artifact == nil {
		s.log.Error("artifact is nil")

		return ""
	}

	return fmt.Sprintf("%s/networks/%s/checks/%s/%s.%s", s.prefix, artifact.Network, artifact.Client, artifact.CheckID, artifact.Type)
}

func (s *ChecksRepo) getArtifact(ctx context.Context, key string) (*CheckArtifact, error) {
	output, err := s.store.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get artifact: %w", err)
	}

	defer output.Body.Close()

	var artifact CheckArtifact
	if err := json.NewDecoder(output.Body).Decode(&artifact); err != nil {
		return nil, fmt.Errorf("failed to decode artifact: %w", err)
	}

	return &artifact, nil
}

// GetBucket returns the S3 bucket name.
func (s *ChecksRepo) GetBucket() string {
	return s.bucket
}

// GetPrefix returns the S3 prefix.
func (s *ChecksRepo) GetPrefix() string {
	return s.prefix
}

// GetStore returns the S3 client.
func (s *ChecksRepo) GetStore() *s3.Client {
	return s.store
}

// GetArtifact retrieves an artifact from S3.
func (s *ChecksRepo) GetArtifact(ctx context.Context, network, client, checkID, artifactType string) (*CheckArtifact, error) {
	defer s.trackDuration("get", "checks")()

	key := fmt.Sprintf("%s/networks/%s/checks/%s/%s.%s", s.prefix, network, client, checkID, artifactType)

	output, err := s.store.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		s.observeOperation("get", "checks", err)

		return nil, fmt.Errorf("failed to get artifact: %w", err)
	}

	defer output.Body.Close()

	// Read the content
	content, err := io.ReadAll(output.Body)
	if err != nil {
		s.observeOperation("get", "checks", err)

		return nil, fmt.Errorf("failed to read artifact content: %w", err)
	}

	s.observeOperation("get", "checks", nil)
	s.metrics.objectSizeBytes.WithLabelValues("checks").Observe(float64(len(content)))

	return &CheckArtifact{
		Network:   network,
		Client:    client,
		CheckID:   checkID,
		Type:      artifactType,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Content:   content,
	}, nil
}
