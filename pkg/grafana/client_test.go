package grafana

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuery(t *testing.T) {
	successResponse := &QueryResponse{
		Results: QueryResults{
			PandaPulse: QueryPandaPulse{
				Frames: []QueryFrame{
					{
						Schema: QuerySchema{
							Fields: []QueryField{
								{
									Labels: map[string]string{
										"instance": "test",
									},
								},
							},
						},
						Data: QueryData{
							Values: []interface{}{1.0},
						},
					},
				},
			},
		},
	}

	tests := []struct {
		name          string
		query         string
		mockResponse  interface{}
		mockStatus    int
		expectedError string
	}{
		{
			name:         "successful query",
			query:        "up",
			mockResponse: successResponse,
			mockStatus:   http.StatusOK,
		},
		{
			name:          "api error",
			query:         "invalid",
			mockResponse:  map[string]string{"error": "invalid query"},
			mockStatus:    http.StatusBadRequest,
			expectedError: "unexpected status code 400",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify request
				assert.Equal(t, "/api/ds/query", r.URL.Path)
				assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

				w.WriteHeader(tt.mockStatus)
				_ = json.NewEncoder(w).Encode(tt.mockResponse)
			}))
			defer server.Close()

			client := NewClient(&Config{
				BaseURL:          server.URL,
				PromDatasourceID: "datasource-id",
				Token:            "test-key",
			}, server.Client())

			resp, err := client.Query(context.Background(), tt.query)

			if tt.expectedError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)

				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)

			if tt.mockStatus == http.StatusOK {
				expectedResp, ok := tt.mockResponse.(*QueryResponse)
				require.True(t, ok)
				assert.Equal(t, expectedResp, resp)
			}
		})
	}
}

func TestGetNetworks(t *testing.T) {
	successResponse := &struct {
		Results map[string]struct {
			Frames []QueryFrame `json:"frames"`
		} `json:"results"`
	}{
		Results: map[string]struct {
			Frames []QueryFrame `json:"frames"`
		}{
			"networks": {
				Frames: []QueryFrame{
					{
						Schema: QuerySchema{
							Fields: []QueryField{
								{
									Labels: map[string]string{
										"network": "pectra-devnet-6",
									},
								},
								{
									Labels: map[string]string{
										"network": "pectra-devnet-7",
									},
								},
								{
									Labels: map[string]string{
										"network": "peerdas-devnet-4",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	tests := []struct {
		name             string
		mockResponse     interface{}
		mockStatus       int
		expectedNetworks []string
		expectedError    string
	}{
		{
			name:             "successful networks query",
			mockResponse:     successResponse,
			mockStatus:       http.StatusOK,
			expectedNetworks: []string{"pectra-devnet-6", "pectra-devnet-7", "peerdas-devnet-4"},
		},
		{
			name:          "api error",
			mockResponse:  map[string]string{"error": "internal error"},
			mockStatus:    http.StatusInternalServerError,
			expectedError: "unexpected status code 500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify request
				assert.Equal(t, "/api/ds/query", r.URL.Path)
				assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

				// Verify query payload
				var payload map[string]interface{}
				err := json.NewDecoder(r.Body).Decode(&payload)
				require.NoError(t, err)

				queries, ok := payload["queries"].([]interface{})
				require.True(t, ok, "queries should be []interface{}")
				require.NotEmpty(t, queries)

				query, ok := queries[0].(map[string]interface{})
				require.True(t, ok, "query should be map[string]interface{}")
				assert.Equal(t, "networks", query["refId"])
				assert.Equal(t, "count by (network) (up)", query["expr"])

				w.WriteHeader(tt.mockStatus)
				_ = json.NewEncoder(w).Encode(tt.mockResponse)
			}))
			defer server.Close()

			client := NewClient(&Config{
				BaseURL:          server.URL,
				PromDatasourceID: "datasource-id",
				Token:            "test-key",
			}, server.Client())

			networks, err := client.GetNetworks(context.Background())

			if tt.expectedError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)

				return
			}

			require.NoError(t, err)
			assert.ElementsMatch(t, tt.expectedNetworks, networks)
		})
	}
}
