package store

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	testBucket = "test-bucket"
	testRegion = "us-east-1"
)

type testHelper struct {
	t          *testing.T
	log        *logrus.Logger
	cfg        *S3Config
	localstack testcontainers.Container
	endpoint   string
}

func newTestHelper(t *testing.T) *testHelper {
	t.Helper()

	log := logrus.New()
	log.SetOutput(os.Stdout)

	return &testHelper{
		t:   t,
		log: log,
	}
}

func (h *testHelper) setup(ctx context.Context) {
	h.t.Helper()

	// Start localstack container.
	req := testcontainers.ContainerRequest{
		Image: "localstack/localstack:latest",
		Env: map[string]string{
			"SERVICES":       "s3",
			"DEFAULT_REGION": testRegion,
		},
		ExposedPorts: []string{"4566/tcp"},
		WaitingFor: wait.ForAll(
			wait.ForLog("Ready."),
			wait.ForListeningPort("4566/tcp"),
		),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		h.t.Fatalf("Failed to start localstack: %v", err)
	}

	h.localstack = container

	// Get endpoint.
	mappedPort, err := container.MappedPort(ctx, "4566")
	if err != nil {
		h.t.Fatalf("Failed to get mapped port: %v", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		h.t.Fatalf("Failed to get host: %v", err)
	}

	h.endpoint = fmt.Sprintf("http://%s", net.JoinHostPort(host, mappedPort.Port()))

	// Setup config.
	h.cfg = &S3Config{
		AccessKeyID:     "test",
		SecretAccessKey: "test",
		Bucket:          testBucket,
		Prefix:          "test",
		EndpointURL:     h.endpoint,
		Region:          testRegion,
	}

	// Create test bucket.
	h.createBucket(ctx)
}

func (h *testHelper) teardown(ctx context.Context) {
	h.t.Helper()

	if h.localstack != nil {
		if err := h.localstack.Terminate(ctx); err != nil {
			h.t.Logf("Failed to terminate container: %v", err)
		}
	}
}

func (h *testHelper) createBucket(ctx context.Context) {
	h.t.Helper()
	setupTest(h.t)

	baseRepo, err := NewBaseRepo(ctx, h.log, h.cfg)
	if err != nil {
		h.t.Fatalf("Failed to create base repo: %v", err)
	}

	_, err = baseRepo.store.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(testBucket),
	})
	if err != nil {
		h.t.Fatalf("Failed to create test bucket: %v", err)
	}
}

// createBaseRepo creates a new BaseRepo for testing.
func (h *testHelper) createBaseRepo(ctx context.Context) BaseRepo {
	h.t.Helper()
	setupTest(h.t)

	baseRepo, err := NewBaseRepo(ctx, h.log, h.cfg)
	if err != nil {
		h.t.Fatalf("Failed to create base repo: %v", err)
	}

	return baseRepo
}

func setupTest(t *testing.T) {
	t.Helper()

	prometheus.DefaultRegisterer = prometheus.NewRegistry()
}
