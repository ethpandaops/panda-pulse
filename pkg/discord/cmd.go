package discord

import (
	"crypto/sha256"
	"math"

	"github.com/bwmarrin/discordgo"
)

// Command represents a top-level discord command
type Command interface {
	// Name returns the command name
	Name() string
	// Register registers the command with Discord
	Register(session *discordgo.Session) error
	// Handle handles the command interaction
	Handle(s *discordgo.Session, i *discordgo.InteractionCreate)
}

// Helper to create string pointer
func stringPtr(s string) *string {
	return &s
}

// hashToColor generates a visually distinct, deterministic color int from a string.
func hashToColor(s string) int {
	hash := sha256.Sum256([]byte(s))

	// Map hue to avoid green (90°-180°)
	hue := remapHue(float64(hash[0]) / 255.0)
	saturation := 0.75                                     // Fixed for vibrancy
	lightness := 0.55 + (float64(hash[10]) / 255.0 * 0.15) // Ensures spread-out lightness

	// Convert HSL to RGB
	r, g, b := hslToRGB(hue, lightness, saturation)

	// Convert to int in 0xRRGGBB format.
	return (r << 16) | (g << 8) | b
}

// hslToRGB converts HSL to RGB (0-255 range for each color).
func hslToRGB(h, l, s float64) (int, int, int) {
	var r, g, b float64

	if s == 0 {
		r, g, b = l, l, l // Achromatic.
	} else {
		q := l * (1 + s)
		if l >= 0.5 {
			q = l + s - (l * s)
		}

		p := 2*l - q

		r = hueToRGB(p, q, h+1.0/3.0)
		g = hueToRGB(p, q, h)
		b = hueToRGB(p, q, h-1.0/3.0)
	}

	return int(math.Round(r * 255)), int(math.Round(g * 255)), int(math.Round(b * 255))
}

// hueToRGB is a helper function for HSL to RGB conversion.
func hueToRGB(p, q, t float64) float64 {
	if t < 0 {
		t += 1
	}

	if t > 1 {
		t -= 1
	}

	if t < 1.0/6.0 {
		return p + (q-p)*6*t
	}

	if t < 1.0/2.0 {
		return q
	}

	if t < 2.0/3.0 {
		return p + (q-p)*(2.0/3.0-t)*6
	}

	return p
}

// remapHue ensures the hue avoids green (90°-180°).
func remapHue(h float64) float64 {
	hueDegrees := h * 360.0

	// If in green range (90-180°), shift to a non-green area.
	if hueDegrees >= 90.0 && hueDegrees <= 180.0 {
		hueDegrees = 180.0 + (hueDegrees - 90.0) // Shift it to the blue/purple spectrum.
	}

	return hueDegrees / 360.0 // Normalize back to 0-1 range.
}
