package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMentionsRepo(t *testing.T) {
	ctx := context.Background()
	helper := newTestHelper(t)
	helper.setup(ctx)
	defer helper.teardown(ctx)

	t.Run("NewMentionsRepo", func(t *testing.T) {
		setupTest(t)
		repo, err := NewMentionsRepo(ctx, helper.log, helper.cfg)
		require.NoError(t, err)
		require.NotNil(t, repo)
	})

	t.Run("List_Empty", func(t *testing.T) {
		setupTest(t)
		repo, err := NewMentionsRepo(ctx, helper.log, helper.cfg)
		require.NoError(t, err)

		mentions, err := repo.List(ctx)
		require.NoError(t, err)
		assert.Empty(t, mentions)
	})

	t.Run("Persist_And_List", func(t *testing.T) {
		setupTest(t)
		repo, err := NewMentionsRepo(ctx, helper.log, helper.cfg)
		require.NoError(t, err)

		mention := &ClientMention{
			Network:   "test-net",
			Client:    "test-client",
			Mentions:  []string{"@test-user", "@test-role"},
			Enabled:   true,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}

		err = repo.Persist(ctx, mention)
		require.NoError(t, err)

		mentions, err := repo.List(ctx)
		require.NoError(t, err)
		require.Len(t, mentions, 1)

		assert.Equal(t, mention.Network, mentions[0].Network)
		assert.Equal(t, mention.Client, mentions[0].Client)
		assert.Equal(t, mention.Mentions, mentions[0].Mentions)
		assert.Equal(t, mention.Enabled, mentions[0].Enabled)
	})

	t.Run("Get_NonExistent", func(t *testing.T) {
		setupTest(t)
		repo, err := NewMentionsRepo(ctx, helper.log, helper.cfg)
		require.NoError(t, err)

		mention, err := repo.Get(ctx, "non-existent-net", "non-existent-client")
		require.NoError(t, err) // Should not error due to our default return
		require.NotNil(t, mention)
		assert.Equal(t, "non-existent-net", mention.Network)
		assert.Equal(t, "non-existent-client", mention.Client)
		assert.Empty(t, mention.Mentions)
		assert.False(t, mention.Enabled)
	})

	t.Run("Get_Existing", func(t *testing.T) {
		setupTest(t)
		repo, err := NewMentionsRepo(ctx, helper.log, helper.cfg)
		require.NoError(t, err)

		original := &ClientMention{
			Network:   "test-net",
			Client:    "test-client",
			Mentions:  []string{"@test-user"},
			Enabled:   true,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}

		err = repo.Persist(ctx, original)
		require.NoError(t, err)

		retrieved, err := repo.Get(ctx, original.Network, original.Client)
		require.NoError(t, err)
		assert.Equal(t, original.Network, retrieved.Network)
		assert.Equal(t, original.Client, retrieved.Client)
		assert.Equal(t, original.Mentions, retrieved.Mentions)
		assert.Equal(t, original.Enabled, retrieved.Enabled)
	})

	t.Run("Purge", func(t *testing.T) {
		setupTest(t)
		repo, err := NewMentionsRepo(ctx, helper.log, helper.cfg)
		require.NoError(t, err)

		mention := &ClientMention{
			Network: "test-net",
			Client:  "test-client",
		}

		err = repo.Persist(ctx, mention)
		require.NoError(t, err)

		err = repo.Purge(ctx, mention.Network, mention.Client)
		require.NoError(t, err)

		mentions, err := repo.List(ctx)
		require.NoError(t, err)
		assert.Empty(t, mentions)
	})

	t.Run("Purge_Invalid_Identifiers", func(t *testing.T) {
		setupTest(t)
		repo, err := NewMentionsRepo(ctx, helper.log, helper.cfg)
		require.NoError(t, err)

		err = repo.Purge(ctx, "test-net") // Missing client identifier
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected network and client identifiers")
	})

	t.Run("Key_Generation", func(t *testing.T) {
		setupTest(t)
		repo, err := NewMentionsRepo(ctx, helper.log, helper.cfg)
		require.NoError(t, err)

		mention := &ClientMention{
			Network: "test-net",
			Client:  "test-client",
		}

		key := repo.Key(mention)
		assert.Equal(t, "test/networks/test-net/mentions/test-client.json", key)
	})

	t.Run("Key_Nil_Mention", func(t *testing.T) {
		setupTest(t)
		repo, err := NewMentionsRepo(ctx, helper.log, helper.cfg)
		require.NoError(t, err)

		key := repo.Key(nil)
		assert.Empty(t, key)
	})
}
