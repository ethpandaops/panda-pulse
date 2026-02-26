package discord

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAsRoleConfig(t *testing.T) {
	cfg := &Config{}
	rc := cfg.AsRoleConfig()

	t.Run("admin roles are flattened from all variants", func(t *testing.T) {
		// Original server roles.
		assert.True(t, rc.AdminRoles["ef"])
		assert.True(t, rc.AdminRoles["admin"])
		assert.True(t, rc.AdminRoles["mod"])
		assert.True(t, rc.AdminRoles["epf"])

		// New server roles mapped to ef.
		assert.True(t, rc.AdminRoles["eels"])
		assert.True(t, rc.AdminRoles["steel"])
		assert.True(t, rc.AdminRoles["pandas"])
	})

	t.Run("admin roles are lowercased", func(t *testing.T) {
		// Verify no uppercase keys leaked through.
		assert.False(t, rc.AdminRoles["EELS"])
		assert.False(t, rc.AdminRoles["Pandas"])
	})

	t.Run("client roles support multiple role names", func(t *testing.T) {
		assert.Contains(t, rc.ClientRoles["lighthouse"], "sigmaprime")
		assert.Contains(t, rc.ClientRoles["lighthouse"], "lighthouse")

		assert.Contains(t, rc.ClientRoles["prysm"], "prysmatic")
		assert.Contains(t, rc.ClientRoles["prysm"], "prysm")

		assert.Contains(t, rc.ClientRoles["lodestar"], "chainsafe")
		assert.Contains(t, rc.ClientRoles["lodestar"], "lodestar")
	})

	t.Run("ethrex client is present", func(t *testing.T) {
		assert.Contains(t, rc.ClientRoles["ethrex"], "ethrex")
	})

	t.Run("nimbusel maps to both nimbus and nimbusel roles", func(t *testing.T) {
		assert.Contains(t, rc.ClientRoles["nimbusel"], "nimbus")
		assert.Contains(t, rc.ClientRoles["nimbusel"], "nimbusel")
	})

	t.Run("single-name clients still work", func(t *testing.T) {
		assert.Equal(t, []string{"teku"}, rc.ClientRoles["teku"])
		assert.Equal(t, []string{"geth"}, rc.ClientRoles["geth"])
		assert.Equal(t, []string{"reth"}, rc.ClientRoles["reth"])
	})
}
