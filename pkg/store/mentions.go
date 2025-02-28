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

// ClientMention represents a set of mentions for a client on a network.
type ClientMention struct {
	Network   string    `json:"network"`
	Client    string    `json:"client"`
	Mentions  []string  `json:"mentions"` // List of role/user IDs to mention
	Enabled   bool      `json:"enabled"`  // Whether mentions are enabled
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// MentionsRepo implements Repository[*ClientMention].
type MentionsRepo struct {
	BaseRepo
}

// NewMentionsRepo creates a new MentionsRepo.
func NewMentionsRepo(ctx context.Context, log *logrus.Logger, cfg *S3Config) (*MentionsRepo, error) {
	baseRepo, err := NewBaseRepo(ctx, log, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create base repo: %w", err)
	}

	return &MentionsRepo{
		BaseRepo: baseRepo,
	}, nil
}

// List implements Repository[*ClientMention].
func (s *MentionsRepo) List(ctx context.Context) ([]*ClientMention, error) {
	var (
		input = &s3.ListObjectsV2Input{
			Bucket: aws.String(s.bucket),
			Prefix: aws.String(fmt.Sprintf("%s/networks/", s.prefix)),
		}
		mentions  []*ClientMention
		paginator = s3.NewListObjectsV2Paginator(s.store, input)
	)

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list mentions: %w", err)
		}

		for _, obj := range page.Contents {
			if !strings.HasSuffix(*obj.Key, ".json") || !strings.Contains(*obj.Key, "/mentions/") {
				continue
			}

			mention, err := s.getMention(ctx, *obj.Key)
			if err != nil {
				continue
			}

			mentions = append(mentions, mention)
		}
	}

	return mentions, nil
}

// Get retrieves a specific mention by network and client.
func (s *MentionsRepo) Get(ctx context.Context, network, client string) (*ClientMention, error) {
	mention, err := s.getMention(ctx, s.Key(&ClientMention{Network: network, Client: client}))
	if err != nil {
		// Check if this is a NoSuchKey error (404)
		var noSuchKey *types.NoSuchKey

		if errors.As(err, &noSuchKey) {
			// Return a default mention configuration instead of an error.
			return &ClientMention{
				Network:   network,
				Client:    client,
				Mentions:  []string{},
				Enabled:   false,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}, nil
		}

		return nil, fmt.Errorf("failed to get mention: %w", err)
	}

	return mention, nil
}

// Persist implements Repository[*ClientMention].
func (s *MentionsRepo) Persist(ctx context.Context, mention *ClientMention) error {
	data, err := json.Marshal(mention)
	if err != nil {
		return fmt.Errorf("failed to marshal mention: %w", err)
	}

	if _, err = s.store.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.Key(mention)),
		Body:   bytes.NewReader(data),
	}); err != nil {
		return fmt.Errorf("failed to put mention: %w", err)
	}

	return nil
}

// Purge implements Repository[*ClientMention].
func (s *MentionsRepo) Purge(ctx context.Context, identifiers ...string) error {
	if len(identifiers) != 2 {
		return fmt.Errorf("expected network and client identifiers, got %d identifiers", len(identifiers))
	}

	network, client := identifiers[0], identifiers[1]

	if _, err := s.store.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.Key(&ClientMention{Network: network, Client: client})),
	}); err != nil {
		return fmt.Errorf("failed to delete mention: %w", err)
	}

	return nil
}

// Key implements Repository[*ClientMention].
func (s *MentionsRepo) Key(mention *ClientMention) string {
	if mention == nil {
		return ""
	}

	return fmt.Sprintf("%s/networks/%s/mentions/%s.json", s.prefix, mention.Network, mention.Client)
}

func (s *MentionsRepo) getMention(ctx context.Context, key string) (*ClientMention, error) {
	output, err := s.store.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get mention: %w", err)
	}

	defer output.Body.Close()

	var mention ClientMention
	if err := json.NewDecoder(output.Body).Decode(&mention); err != nil {
		return nil, fmt.Errorf("failed to decode mention: %w", err)
	}

	return &mention, nil
}
