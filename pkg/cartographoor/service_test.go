package cartographoor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestCartographoorService(t *testing.T) {
	// Set up a mock server with test data
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"networkMetadata": {
				"eof": {
					"displayName": "EOF Devnets",
					"description": "EOF test network",
					"stats": {
						"totalNetworks": 1,
						"activeNetworks": 1,
						"inactiveNetworks": 0,
						"networkNames": ["devnet-0"]
					}
				}
			},
			"networks": {
				"eof-devnet-0": {
					"name": "devnet-0",
					"repository": "ethpandaops/eof-devnets",
					"status": "active",
					"chainId": 7023642286
				},
				"pectra-devnet-1": {
					"name": "devnet-1",
					"repository": "ethpandaops/pectra-devnets",
					"status": "inactive",
					"chainId": 7023642287
				},
				"mainnet": {
					"name": "mainnet",
					"description": "Production Ethereum network",
					"status": "active",
					"chainId": 1
				},
				"sepolia": {
					"name": "sepolia",
					"description": "Smaller testnet for application development",
					"status": "active",
					"chainId": 11155111
				},
				"inactive-test": {
					"name": "inactive",
					"status": "inactive"
				}
			},
			"clients": {
				"geth": {
					"name": "geth",
					"displayName": "Geth",
					"repository": "ethereum/go-ethereum",
					"type": "execution",
					"branch": "master",
					"logo": "https://ethpandaops.io/img/clients/geth.jpg",
					"latestVersion": "v1.15.11"
				},
				"lighthouse": {
					"name": "lighthouse",
					"displayName": "Lighthouse",
					"repository": "sigp/lighthouse",
					"type": "consensus",
					"branch": "stable",
					"logo": "https://ethpandaops.io/img/clients/lighthouse.jpg",
					"latestVersion": "v7.0.1"
				}
			}
		}`))
	}))
	defer mockServer.Close()

	// Set up a logger for testing
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	// Create a new cartographoor service
	ctx := context.Background()
	service, err := NewService(ctx, ServiceConfig{
		SourceURL:       mockServer.URL,
		RefreshInterval: 1 * time.Hour,
		Logger:          logger,
	})

	assert.NoError(t, err)
	assert.NotNil(t, service)

	// Test client data
	t.Run("Client data", func(t *testing.T) {
		assert.Equal(t, "ethereum/go-ethereum", service.GetClientRepository("geth"))
		assert.Equal(t, "master", service.GetClientBranch("geth"))
		assert.Equal(t, "https://ethpandaops.io/img/clients/geth.jpg", service.GetClientLogo("geth"))
		assert.Equal(t, "v1.15.11", service.GetClientLatestVersion("geth"))
		assert.Equal(t, "Geth", service.GetClientDisplayName("geth"))
		assert.Equal(t, "execution", service.GetClientType("geth"))

		assert.True(t, service.IsELClient("geth"))
		assert.False(t, service.IsCLClient("geth"))

		assert.True(t, service.IsCLClient("lighthouse"))
		assert.False(t, service.IsELClient("lighthouse"))

		assert.Len(t, service.GetAllClients(), 2)
		assert.Len(t, service.GetConsensusClients(), 1)
		assert.Len(t, service.GetExecutionClients(), 1)
	})

	// Test network data with devnet filtering
	t.Run("Network data", func(t *testing.T) {
		// Should only return networks with "devnet" in the name
		activeNetworks := service.GetActiveNetworks()
		assert.Len(t, activeNetworks, 1)
		assert.Contains(t, activeNetworks, "eof-devnet-0")
		// Should not contain mainnet or sepolia since they don't have "devnet" in name
		assert.NotContains(t, activeNetworks, "mainnet")
		assert.NotContains(t, activeNetworks, "sepolia")

		// Should only return networks with "devnet" in the name
		allNetworks := service.GetAllNetworks()
		assert.Len(t, allNetworks, 2)
		assert.Contains(t, allNetworks, "eof-devnet-0")
		assert.Contains(t, allNetworks, "pectra-devnet-1")
		// Should not contain networks without "devnet" in the name
		assert.NotContains(t, allNetworks, "mainnet")
		assert.NotContains(t, allNetworks, "sepolia")
		assert.NotContains(t, allNetworks, "inactive-test")

		// These should still work as expected
		eofNetworks := service.GetNetworksOfType("eof")
		assert.Len(t, eofNetworks, 1)
		assert.Equal(t, "eof-devnet-0", eofNetworks[0])

		// Should return nil for non-devnet networks
		mainnet := service.GetNetwork("mainnet")
		assert.Nil(t, mainnet)

		// Should work for devnet networks
		eofDevnet := service.GetNetwork("eof-devnet-0")
		assert.NotNil(t, eofDevnet)
		assert.Equal(t, "devnet-0", eofDevnet.Name)
		assert.Equal(t, "active", eofDevnet.Status)
		assert.Equal(t, int64(7023642286), eofDevnet.ChainID)

		// Status checks should only work for devnet networks
		assert.Equal(t, "", service.GetNetworkStatus("mainnet"))
		assert.Equal(t, "active", service.GetNetworkStatus("eof-devnet-0"))
		assert.Equal(t, "inactive", service.GetNetworkStatus("pectra-devnet-1"))
	})
}
