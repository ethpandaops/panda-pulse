package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChecksRepo(t *testing.T) {
	ctx := context.Background()
	helper := newTestHelper(t)
	helper.setup(ctx)
	defer helper.teardown(ctx)

	t.Run("NewChecksRepo", func(t *testing.T) {
		setupTest(t)
		repo, err := NewChecksRepo(ctx, helper.log, helper.cfg, NewMetrics("test"))
		require.NoError(t, err)
		require.NotNil(t, repo)
	})

	t.Run("List_Empty", func(t *testing.T) {
		setupTest(t)
		repo, err := NewChecksRepo(ctx, helper.log, helper.cfg, NewMetrics("test"))
		require.NoError(t, err)

		artifacts, err := repo.List(ctx)
		require.NoError(t, err)
		assert.Empty(t, artifacts)
	})

	t.Run("Persist_And_List", func(t *testing.T) {
		setupTest(t)
		repo, err := NewChecksRepo(ctx, helper.log, helper.cfg, NewMetrics("test"))
		require.NoError(t, err)

		artifact := &CheckArtifact{
			Network:   "test-net",
			Client:    "test-client",
			CheckID:   "test-check",
			Type:      "log",
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
			Content:   []byte("test log content"),
		}

		err = repo.Persist(ctx, artifact)
		require.NoError(t, err)

		artifacts, err := repo.List(ctx)
		require.NoError(t, err)
		require.Len(t, artifacts, 1)

		assert.Equal(t, artifact.Network, artifacts[0].Network)
		assert.Equal(t, artifact.Client, artifacts[0].Client)
		assert.Equal(t, artifact.CheckID, artifacts[0].CheckID)
		assert.Equal(t, artifact.Type, artifacts[0].Type)
	})

	t.Run("GetArtifact", func(t *testing.T) {
		setupTest(t)
		repo, err := NewChecksRepo(ctx, helper.log, helper.cfg, NewMetrics("test"))
		require.NoError(t, err)

		content := []byte("test log content")
		artifact := &CheckArtifact{
			Network:   "test-net",
			Client:    "test-client",
			CheckID:   "test-check",
			Type:      "log",
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
			Content:   content,
		}

		err = repo.Persist(ctx, artifact)
		require.NoError(t, err)

		retrieved, err := repo.GetArtifact(ctx, artifact.Network, artifact.Client, artifact.CheckID, artifact.Type)
		require.NoError(t, err)
		assert.Equal(t, content, retrieved.Content)
	})

	t.Run("Purge", func(t *testing.T) {
		setupTest(t)
		repo, err := NewChecksRepo(ctx, helper.log, helper.cfg, NewMetrics("test"))
		require.NoError(t, err)

		artifact := &CheckArtifact{
			Network: "test-net",
			Client:  "test-client",
			CheckID: "test-check",
			Type:    "log",
		}

		err = repo.Persist(ctx, artifact)
		require.NoError(t, err)

		err = repo.Purge(ctx, artifact.Network, artifact.Client, artifact.CheckID)
		require.NoError(t, err)

		artifacts, err := repo.List(ctx)
		require.NoError(t, err)
		assert.Empty(t, artifacts)
	})

	t.Run("Purge_Invalid_Identifiers", func(t *testing.T) {
		setupTest(t)
		repo, err := NewChecksRepo(ctx, helper.log, helper.cfg, NewMetrics("test"))
		require.NoError(t, err)

		err = repo.Purge(ctx, "test-net", "test-client") // Missing checkID
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected network, client and checkID identifiers")
	})

	t.Run("Key_Generation", func(t *testing.T) {
		setupTest(t)
		repo, err := NewChecksRepo(ctx, helper.log, helper.cfg, NewMetrics("test"))
		require.NoError(t, err)

		artifact := &CheckArtifact{
			Network: "test-net",
			Client:  "test-client",
			CheckID: "test-check",
			Type:    "log",
		}

		key := repo.Key(artifact)
		assert.Equal(t, "test/networks/test-net/checks/test-client/test-check.log", key)
	})

	t.Run("Key_Nil_Artifact", func(t *testing.T) {
		setupTest(t)
		repo, err := NewChecksRepo(ctx, helper.log, helper.cfg, NewMetrics("test"))
		require.NoError(t, err)

		key := repo.Key(nil)
		assert.Empty(t, key)
	})

	t.Run("GetBucket", func(t *testing.T) {
		setupTest(t)
		repo, err := NewChecksRepo(ctx, helper.log, helper.cfg, NewMetrics("test"))
		require.NoError(t, err)
		assert.Equal(t, testBucket, repo.GetBucket())
	})

	t.Run("GetPrefix", func(t *testing.T) {
		setupTest(t)
		repo, err := NewChecksRepo(ctx, helper.log, helper.cfg, NewMetrics("test"))
		require.NoError(t, err)
		assert.Equal(t, "test", repo.GetPrefix())
	})

	t.Run("GetStore", func(t *testing.T) {
		setupTest(t)
		repo, err := NewChecksRepo(ctx, helper.log, helper.cfg, NewMetrics("test"))
		require.NoError(t, err)
		assert.NotNil(t, repo.GetStore())
	})
}
