package checks

import (
	"context"
	"testing"

	"github.com/ethpandaops/panda-pulse/pkg/clients"
	"github.com/ethpandaops/panda-pulse/pkg/grafana"
	"github.com/ethpandaops/panda-pulse/pkg/grafana/mock"
	"github.com/ethpandaops/panda-pulse/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestHeadSlotCheck_Run(t *testing.T) {
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
			name: "all nodes advancing",
			config: Config{
				Network:       "mainnet",
				ConsensusNode: "lighthouse",
				ExecutionNode: "geth",
			},
			mockResponse:   &grafana.QueryResponse{},
			expectedStatus: StatusOK,
		},
		{
			name: "nodes not advancing",
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

			mockClient := mock.NewMockClient(ctrl)
			mockClient.EXPECT().Query(gomock.Any(), gomock.Any()).Return(tt.mockResponse, tt.mockError)

			log := logger.NewCheckLogger("id")
			check := NewHeadSlotCheck(mockClient)
			result, err := check.Run(context.Background(), log, tt.config)

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

func TestHeadSlotCheck_Name(t *testing.T) {
	check := NewHeadSlotCheck(nil)
	assert.Equal(t, "Head slot not advancing", check.Name())
}

func TestHeadSlotCheck_Category(t *testing.T) {
	check := NewHeadSlotCheck(nil)
	assert.Equal(t, CategorySync, check.Category())
}

func TestHeadSlotCheck_ClientType(t *testing.T) {
	check := NewHeadSlotCheck(nil)
	assert.Equal(t, clients.ClientTypeCL, check.ClientType())
}
