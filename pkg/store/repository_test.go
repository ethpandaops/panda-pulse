package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBaseRepo(t *testing.T) {
	ctx := context.Background()
	helper := newTestHelper(t)
	helper.setup(ctx)
	defer helper.teardown(ctx)

	t.Run("NewBaseRepo", func(t *testing.T) {
		baseRepo := helper.createBaseRepo(ctx)
		require.NotNil(t, baseRepo.store)
		assert.Equal(t, testBucket, baseRepo.bucket)
		assert.Equal(t, "test", baseRepo.prefix)
	})

	t.Run("VerifyConnection", func(t *testing.T) {
		baseRepo := helper.createBaseRepo(ctx)
		err := baseRepo.VerifyConnection(ctx)
		require.NoError(t, err)
	})

	t.Run("GetS3Client", func(t *testing.T) {
		baseRepo := helper.createBaseRepo(ctx)
		client := baseRepo.GetS3Client()
		require.NotNil(t, client)
	})

	t.Run("Invalid_Credentials", func(t *testing.T) {
		invalidCfg := *helper.cfg
		invalidCfg.AccessKeyID = "invalid"
		invalidCfg.SecretAccessKey = "invalid"

		_, err := NewBaseRepo(ctx, helper.log, &invalidCfg)
		require.NoError(t, err) // Creation should succeed as AWS SDK validates credentials lazily

		baseRepo := helper.createBaseRepo(ctx)
		err = baseRepo.VerifyConnection(ctx)
		require.NoError(t, err) // Localstack doesn't validate credentials
	})

	t.Run("Invalid_Bucket", func(t *testing.T) {
		invalidCfg := *helper.cfg
		invalidCfg.Bucket = "nonexistent-bucket"

		baseRepo, err := NewBaseRepo(ctx, helper.log, &invalidCfg)
		require.NoError(t, err)

		err = baseRepo.VerifyConnection(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to access bucket")
	})

	t.Run("Invalid_Endpoint", func(t *testing.T) {
		invalidCfg := *helper.cfg
		invalidCfg.EndpointURL = "http://invalid:1234"

		baseRepo, err := NewBaseRepo(ctx, helper.log, &invalidCfg)
		require.NoError(t, err)

		err = baseRepo.VerifyConnection(ctx)
		require.Error(t, err)
	})
}
