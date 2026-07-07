package build

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComputeBaseDockerTag(t *testing.T) {
	tests := []struct {
		name               string
		repository         string
		ref                string
		dockerTagOverride  string
		upstreamRepository string
		expected           string
	}{
		{
			name:               "uppercase branch name is lowercased",
			repository:         "bomanaps/grandine",
			ref:                "feature/FCR",
			upstreamRepository: "grandinetech/grandine",
			expected:           "bomanaps-feature-fcr",
		},
		{
			name:               "uppercase docker tag override is lowercased",
			repository:         "grandinetech/grandine",
			ref:                "master",
			dockerTagOverride:  "My-Custom-TAG",
			upstreamRepository: "grandinetech/grandine",
			expected:           "my-custom-tag",
		},
		{
			name:               "repository comparison is case-insensitive",
			repository:         "GrandineTech/Grandine",
			ref:                "master",
			upstreamRepository: "grandinetech/grandine",
			expected:           "master",
		},
		{
			name:               "fork prefix is lowercased",
			repository:         "BomanAps/grandine",
			ref:                "fix/bug#123",
			upstreamRepository: "grandinetech/grandine",
			expected:           "bomanaps-fix-bug-123",
		},
		{
			name:               "no prefix when override is set",
			repository:         "bomanaps/grandine",
			ref:                "feature/FCR",
			dockerTagOverride:  "custom",
			upstreamRepository: "grandinetech/grandine",
			expected:           "custom",
		},
		{
			name:               "leading dashes trimmed after sanitization",
			repository:         "grandinetech/grandine",
			ref:                "/Fix",
			upstreamRepository: "grandinetech/grandine",
			expected:           "fix",
		},
		{
			name:     "empty input returns empty",
			ref:      "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeBaseDockerTag(tt.repository, tt.ref, tt.dockerTagOverride, tt.upstreamRepository)
			assert.Equal(t, tt.expected, got)
		})
	}
}
