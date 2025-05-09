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
