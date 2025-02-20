package service

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/ethpandaops/panda-pulse/pkg/discord/mock"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/mock/gomock"
)

type testHelper struct {
	t          *testing.T
	log        *logrus.Logger
	localstack testcontainers.Container
	endpoint   string
	s3Client   *s3.Client
}

func newTestHelper(t *testing.T) *testHelper {
	t.Helper()

	return &testHelper{
		t:   t,
		log: logrus.New(),
	}
}

func (h *testHelper) setup(ctx context.Context) {
	h.t.Helper()

	// Start localstack container
	req := testcontainers.ContainerRequest{
		Image: "localstack/localstack:latest",
		Env: map[string]string{
			"SERVICES":              "s3",
			"DEFAULT_REGION":        "us-east-1",
			"EAGER_SERVICE_LOADING": "1",
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

	// Get endpoint
	mappedPort, err := container.MappedPort(ctx, "4566")
	if err != nil {
		h.t.Fatalf("Failed to get mapped port: %v", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		h.t.Fatalf("Failed to get host: %v", err)
	}

	h.endpoint = "http://" + net.JoinHostPort(host, mappedPort.Port())

	// Create S3 client.
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
	)
	if err != nil {
		h.t.Fatalf("Failed to create AWS config: %v", err)
	}

	h.s3Client = s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = true
		o.BaseEndpoint = aws.String(h.endpoint)
	})

	// Create test bucket
	if _, err = h.s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String("test-bucket"),
	}); err != nil {
		h.t.Fatalf("Failed to create test bucket: %v", err)
	}

	// Wait a bit for S3 to be ready
	time.Sleep(2 * time.Second)
}

func (h *testHelper) teardown(ctx context.Context) {
	h.t.Helper()

	if h.localstack != nil {
		if err := h.localstack.Terminate(ctx); err != nil {
			h.t.Logf("Failed to terminate container: %v", err)
		}
	}
}

func (h *testHelper) createConfig() *Config {
	return &Config{
		HealthCheckAddress: ":9191",
		MetricsAddress:     ":9091",
		GrafanaToken:       "test-grafana-token",
		DiscordToken:       "test-discord-token",
		GrafanaBaseURL:     "http://localhost",
		PromDatasourceID:   "test-datasource",
		AccessKeyID:        "test",
		SecretAccessKey:    "test",
		S3Bucket:           "test-bucket",
		S3BucketPrefix:     "test",
		S3Region:           "us-east-1",
		S3EndpointURL:      h.endpoint,
	}
}

func TestService(t *testing.T) {
	ctx := context.Background()
	helper := newTestHelper(t)
	helper.setup(ctx)
	defer helper.teardown(ctx)

	t.Run("Start_And_Stop", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		cfg := helper.createConfig()
		svc, err := NewService(ctx, helper.log, cfg)
		require.NoError(t, err)

		// Replace the real bot with our mock
		mockBot := mock.NewMockBot(ctrl)

		// Set expectations
		mockBot.EXPECT().Start().Return(nil)
		mockBot.EXPECT().Stop().Return(nil)
		mockBot.EXPECT().GetGrafana().Return(nil).AnyTimes()
		mockBot.EXPECT().GetMonitorRepo().Return(nil).AnyTimes()
		mockBot.EXPECT().GetChecksRepo().Return(nil).AnyTimes()
		mockBot.EXPECT().GetScheduler().Return(nil).AnyTimes()
		mockBot.EXPECT().GetSession().Return(nil).AnyTimes()
		mockBot.EXPECT().GetHive().Return(nil).AnyTimes()

		svc.bot = mockBot

		// Start service
		err = svc.Start(ctx)
		require.NoError(t, err)

		// Small delay to ensure servers are ready
		time.Sleep(1 * time.Second)

		// Verify health endpoint is working
		healthClient := &http.Client{Timeout: 5 * time.Second}
		resp, err := healthClient.Get("http://127.0.0.1:9191/healthz")
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		resp.Body.Close()

		// Verify metrics endpoint is working
		metricsClient := &http.Client{Timeout: 5 * time.Second}
		resp, err = metricsClient.Get("http://127.0.0.1:9091/metrics")
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		resp.Body.Close()

		// Stop service.
		require.NoError(t, svc.Stop(ctx))
	})
}
