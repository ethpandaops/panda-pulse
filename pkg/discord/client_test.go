package discord

import (
	"testing"
)

func TestHashToColor(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantType int
		// Each network should produce a consistent color.
		wantConsistent bool
		// Color shouldn't be green (no values between 0x009900 and 0x00FF00).
		wantNotGreen bool
	}{
		{
			name:           "devnet-1",
			input:          "devnet-1",
			wantType:       0,
			wantConsistent: true,
			wantNotGreen:   true,
		},
		{
			name:           "empty string",
			input:          "",
			wantType:       0,
			wantConsistent: true,
			wantNotGreen:   true,
		},
		{
			name:           "mainnet",
			input:          "mainnet",
			wantType:       0,
			wantConsistent: true,
			wantNotGreen:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test consistency.
			if tt.wantConsistent {
				color1 := hashToColor(tt.input)
				color2 := hashToColor(tt.input)
				if color1 != color2 {
					t.Errorf("hashToColor not consistent for input %q: got %06x and %06x", tt.input, color1, color2)
				}
			}

			// Test color format.
			got := hashToColor(tt.input)
			if got < 0 || got > 0xFFFFFF {
				t.Errorf("hashToColor(%q) = %06x, want color between 000000 and FFFFFF", tt.input, got)
			}

			// Test not green.
			if tt.wantNotGreen {
				r := (got >> 16) & 0xFF
				g := (got >> 8) & 0xFF
				b := got & 0xFF

				// Check if the color is predominantly green.
				if g > r+50 && g > b+50 {
					t.Errorf("hashToColor(%q) = %06x produced a green color (R:%02x, G:%02x, B:%02x)", tt.input, got, r, g, b)
				}
			}
		})
	}
}
