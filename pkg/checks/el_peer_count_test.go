package checks

import (
	"context"
	"testing"

	"github.com/ethpandaops/panda-pulse/pkg/grafana"
	"github.com/ethpandaops/panda-pulse/pkg/grafana/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestELPeerCountCheck_Run(t *testing.T) {
	failingResponse := &grafana.QueryResponse{
		Results: grafana.QueryResults{
			PandaPulse: grafana.QueryPandaPulse{
				Frames: []grafana.QueryFrame{
					{
						Schema: grafana.QuerySchema{
							Fields: []grafana.QueryField{
								{
									Labels: map[string]string{
										"instance": "node1",
										"network":  "mainnet",
									},
								},
							},
						},
						Data: grafana.QueryData{
							Values: []interface{}{1.0},
						},
					},
				},
			},
		},
	}

	tests := []struct {
		name           string
		config         Config
		mockResponse   *grafana.QueryResponse
		mockError      error
		expectedStatus Status
		expectError    bool
	}{
		{
			name: "all nodes have sufficient peers",
			config: Config{
				Network:       "mainnet",
				ConsensusNode: "lighthouse",
				ExecutionNode: "geth",
			},
			mockResponse:   &grafana.QueryResponse{},
			expectedStatus: StatusOK,
		},
		{
			name: "nodes with low peer count",
			config: Config{
				Network:       "mainnet",
				ConsensusNode: "lighthouse",
				ExecutionNode: "geth",
			},
			mockResponse:   failingResponse,
			expectedStatus: StatusFail,
		},
		{
			name: "grafana error",
			config: Config{
				Network:       "mainnet",
				ConsensusNode: "lighthouse",
				ExecutionNode: "geth",
			},
			mockError:   assert.AnError,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockClient := mock.NewMockGrafanaClient(ctrl)
			mockClient.EXPECT().Query(gomock.Any(), gomock.Any()).Return(tt.mockResponse, tt.mockError)

			check := NewELPeerCountCheck(mockClient)
			result, err := check.Run(context.Background(), tt.config)

			if tt.expectError {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedStatus, result.Status)
			assert.NotEmpty(t, result.Description)
			assert.NotNil(t, result.Details)
			assert.Contains(t, result.Details, "query")
		})
	}
}

func TestELPeerCountCheck_Name(t *testing.T) {
	check := NewELPeerCountCheck(nil)
	assert.Equal(t, "Low peer count", check.Name())
}

func TestELPeerCountCheck_Category(t *testing.T) {
	check := NewELPeerCountCheck(nil)
	assert.Equal(t, CategorySync, check.Category())
}

func TestELPeerCountCheck_ClientType(t *testing.T) {
	check := NewELPeerCountCheck(nil)
	assert.Equal(t, ClientTypeEL, check.ClientType())
}
