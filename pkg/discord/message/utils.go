package message

import (
	"crypto/sha256"
	"math"
	"strings"
)

// hashToColor generates a visually distinct, deterministic color int from a string.
// This is then used for the discord alert to color code alerts for different networks.
func hashToColor(s string) int {
	// Extract the network identifier (e.g., "peerdas" from "peerdas-network-5").
	parts := strings.Split(s, "-")
	if len(parts) == 0 {
		return 0
	}

	// Use the network identifier and number for better distribution.
	var (
		identifier = parts[0]
		number     = "0"
	)

	if len(parts) > 2 {
		number = parts[len(parts)-1]
	}

	// Create a unique string that emphasizes the differences.
	uniqueStr := identifier + number

	hash := sha256.Sum256([]byte(uniqueStr))

	// Use first byte for base hue selection (0-5 for 6 distinct base colors).
	baseHue := float64(hash[0]%6) / 6.0

	// Use second byte for slight hue variation within the base color.
	hueVariation := float64(hash[1]) / 255.0 / 12.0 // Small variation (1/12 of the way to the next color)

	hue := baseHue + hueVariation

	// Fixed saturation and lightness for good visibility.
	saturation := 0.75
	lightness := 0.60

	// Convert HSL to RGB.
	r, g, b := hslToRGB(hue, lightness, saturation)

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
