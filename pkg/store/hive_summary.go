package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/ethpandaops/panda-pulse/pkg/hive"
	"github.com/sirupsen/logrus"
)

// HiveSummaryRepo implements Repository for Hive summary alerts.
type HiveSummaryRepo struct {
	BaseRepo
}

// NewHiveSummaryRepo creates a new HiveSummaryRepo.
func NewHiveSummaryRepo(ctx context.Context, log *logrus.Logger, cfg *S3Config, metrics *Metrics) (*HiveSummaryRepo, error) {
	baseRepo, err := NewBaseRepo(ctx, log, cfg, metrics)
	if err != nil {
		return nil, fmt.Errorf("failed to create base repo: %w", err)
	}

	return &HiveSummaryRepo{
		BaseRepo: baseRepo,
	}, nil
}

// List implements Repository for Hive summary alerts.
func (s *HiveSummaryRepo) List(ctx context.Context) ([]*hive.HiveSummaryAlert, error) {
	defer s.trackDuration("list", "hive_summary")()

	var (
		alerts []*hive.HiveSummaryAlert
		input  = &s3.ListObjectsV2Input{
			Bucket: aws.String(s.bucket),
			Prefix: aws.String(fmt.Sprintf("%s/networks/", s.prefix)),
		}
		paginator = s3.NewListObjectsV2Paginator(s.store, input)
	)

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			s.observeOperation("list", "hive_summary", err)

			return nil, fmt.Errorf("failed to list alerts: %w", err)
		}

		for _, obj := range page.Contents {
			if !strings.HasSuffix(*obj.Key, ".json") || !strings.Contains(*obj.Key, "/hive_summary/") {
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

	s.metrics.objectsTotal.WithLabelValues("hive_summary").Set(float64(len(alerts)))

	return alerts, nil
}

// Persist implements Repository for Hive summary alerts.
func (s *HiveSummaryRepo) Persist(ctx context.Context, alert *hive.HiveSummaryAlert) error {
	defer s.trackDuration("persist", "hive_summary")()

	data, err := json.Marshal(alert)
	if err != nil {
		s.observeOperation("persist", "hive_summary", err)

		return fmt.Errorf("failed to marshal alert: %w", err)
	}

	s.metrics.objectSizeBytes.WithLabelValues("hive_summary").Observe(float64(len(data)))

	if _, err = s.store.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.Key(alert)),
		Body:   bytes.NewReader(data),
	}); err != nil {
		s.observeOperation("persist", "hive_summary", err)

		return fmt.Errorf("failed to put alert: %w", err)
	}

	s.observeOperation("persist", "hive_summary", nil)

	return nil
}

// Purge implements Repository for Hive summary alerts.
func (s *HiveSummaryRepo) Purge(ctx context.Context, identifiers ...string) error {
	if len(identifiers) < 1 || len(identifiers) > 2 {
		return fmt.Errorf("expected network and optional suite identifiers, got %d identifiers", len(identifiers))
	}

	network := identifiers[0]
	suite := ""

	if len(identifiers) == 2 {
		suite = identifiers[1]
	}

	if _, err := s.store.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.Key(&hive.HiveSummaryAlert{Network: network, Suite: suite})),
	}); err != nil {
		return fmt.Errorf("failed to delete alert: %w", err)
	}

	return nil
}

// Key implements Repository for Hive summary alerts.
func (s *HiveSummaryRepo) Key(alert *hive.HiveSummaryAlert) string {
	if alert == nil {
		s.log.Error("alert is nil")

		return ""
	}

	// Include suite in path if specified
	if alert.Suite != "" {
		return fmt.Sprintf("%s/networks/%s/hive_summary/%s/alert.json", s.prefix, alert.Network, alert.Suite)
	}

	return fmt.Sprintf("%s/networks/%s/hive_summary/alert.json", s.prefix, alert.Network)
}

// GetByNetwork retrieves a Hive summary alert by network.
func (s *HiveSummaryRepo) GetByNetwork(ctx context.Context, network string) (*hive.HiveSummaryAlert, error) {
	defer s.trackDuration("get", "hive_summary")()

	key := fmt.Sprintf("%s/networks/%s/hive_summary/alert.json", s.prefix, network)

	alert, err := s.getAlert(ctx, key)
	if err != nil {
		s.observeOperation("get", "hive_summary", err)

		return nil, err
	}

	s.observeOperation("get", "hive_summary", nil)

	return alert, nil
}

// GetByNetworkAndSuite retrieves a Hive summary alert by network and suite.
func (s *HiveSummaryRepo) GetByNetworkAndSuite(ctx context.Context, network, suite string) (*hive.HiveSummaryAlert, error) {
	defer s.trackDuration("get", "hive_summary")()

	var key string
	if suite != "" {
		key = fmt.Sprintf("%s/networks/%s/hive_summary/%s/alert.json", s.prefix, network, suite)
	} else {
		key = fmt.Sprintf("%s/networks/%s/hive_summary/alert.json", s.prefix, network)
	}

	alert, err := s.getAlert(ctx, key)
	if err != nil {
		s.observeOperation("get", "hive_summary", err)

		return nil, err
	}

	s.observeOperation("get", "hive_summary", nil)

	return alert, nil
}

func (s *HiveSummaryRepo) getAlert(ctx context.Context, key string) (*hive.HiveSummaryAlert, error) {
	output, err := s.store.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get alert: %w", err)
	}

	defer output.Body.Close()

	var alert hive.HiveSummaryAlert
	if err := json.NewDecoder(output.Body).Decode(&alert); err != nil {
		return nil, fmt.Errorf("failed to decode alert: %w", err)
	}

	return &alert, nil
}

// StoreSummaryResult stores a summary result for historical tracking.
func (s *HiveSummaryRepo) StoreSummaryResult(ctx context.Context, result *hive.SummaryResult) error {
	return s.StoreSummaryResultWithSuite(ctx, result, "")
}

// StoreSummaryResultWithSuite stores a summary result for historical tracking with suite filter.
func (s *HiveSummaryRepo) StoreSummaryResultWithSuite(ctx context.Context, result *hive.SummaryResult, suite string) error {
	defer s.trackDuration("persist", "hive_summary_result")()

	if result == nil {
		return fmt.Errorf("result is nil")
	}

	// Format date as YYYY-MM-DD using the timestamp from the result
	// This ensures we store it under the date the tests were actually run
	dateStr := result.Timestamp.Format("2006-01-02")

	var key string
	if suite != "" {
		key = fmt.Sprintf("%s/networks/%s/hive_summary/%s/results/%s.json", s.prefix, result.Network, suite, dateStr)
	} else {
		key = fmt.Sprintf("%s/networks/%s/hive_summary/results/%s.json", s.prefix, result.Network, dateStr)
	}

	data, err := json.Marshal(result)
	if err != nil {
		s.observeOperation("persist", "hive_summary_result", err)

		return fmt.Errorf("failed to marshal result: %w", err)
	}

	s.metrics.objectSizeBytes.WithLabelValues("hive_summary_result").Observe(float64(len(data)))

	if _, err = s.store.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	}); err != nil {
		s.observeOperation("persist", "hive_summary_result", err)

		return fmt.Errorf("failed to put result: %w", err)
	}

	s.observeOperation("persist", "hive_summary_result", nil)

	return nil
}

// GetPreviousSummaryResult retrieves the previous summary result.
func (s *HiveSummaryRepo) GetPreviousSummaryResult(ctx context.Context, network string) (*hive.SummaryResult, error) {
	return s.GetPreviousSummaryResultWithSuite(ctx, network, "")
}

// GetPreviousSummaryResultWithSuite retrieves the previous summary result with suite filter.
func (s *HiveSummaryRepo) GetPreviousSummaryResultWithSuite(ctx context.Context, network, suite string) (*hive.SummaryResult, error) {
	defer s.trackDuration("get", "hive_summary_result")()

	// List all summary results for this network
	var prefix string
	if suite != "" {
		prefix = fmt.Sprintf("%s/networks/%s/hive_summary/%s/results/", s.prefix, network, suite)
	} else {
		prefix = fmt.Sprintf("%s/networks/%s/hive_summary/results/", s.prefix, network)
	}

	output, err := s.store.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	})
	if err != nil {
		s.observeOperation("get", "hive_summary_result", err)

		return nil, fmt.Errorf("failed to list summary results: %w", err)
	}

	if len(output.Contents) == 0 {
		return nil, fmt.Errorf("no previous summary results found")
	}

	// Map to store date -> key for sorting.
	var (
		dateKeys = make(map[string]string)
		dates    = make([]string, 0)
	)

	// Extract dates from filenames.
	for _, obj := range output.Contents {
		key := *obj.Key

		parts := strings.Split(key, "/")
		if len(parts) == 0 {
			continue
		}

		filename := parts[len(parts)-1]
		if !strings.HasSuffix(filename, ".json") {
			continue
		}

		date := strings.TrimSuffix(filename, ".json")
		if _, parseErr := time.Parse("2006-01-02", date); parseErr != nil {
			continue
		}

		dateKeys[date] = key

		dates = append(dates, date)
	}

	if len(dates) == 0 {
		return nil, fmt.Errorf("no valid summary results found")
	}

	// Sort dates in descending order (newest first)
	sort.Strings(dates)
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))

	s.log.WithField("dates", dates).Debug("Found summary result dates")

	// If we only have one result, we can't get a "previous" one
	if len(dates) < 2 {
		return nil, fmt.Errorf("only one summary result found, need at least two for comparison")
	}

	// Get the second most recent result (index 1 after sorting)
	previousDate := dates[1]
	previousKey := dateKeys[previousDate]

	s.log.WithFields(logrus.Fields{
		"mostRecentDate": dates[0],
		"previousDate":   previousDate,
	}).Debug("Found previous summary result")

	// Get the previous result
	getOutput, err := s.store.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(previousKey),
	})
	if err != nil {
		s.observeOperation("get", "hive_summary_result", err)

		return nil, fmt.Errorf("failed to get previous result: %w", err)
	}

	defer getOutput.Body.Close()

	var result hive.SummaryResult
	if err := json.NewDecoder(getOutput.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode result: %w", err)
	}

	return &result, nil
}
