package logger

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckLogger(t *testing.T) {
	t.Run("NewCheckLogger", func(t *testing.T) {
		id := "test-123"
		logger := NewCheckLogger(id)

		require.NotNil(t, logger)
		assert.Equal(t, id, logger.GetID())
		assert.NotNil(t, logger.GetBuffer())
	})

	t.Run("Printf", func(t *testing.T) {
		logger := NewCheckLogger("test")
		logger.Printf("test message %s", "value")

		output := logger.GetBuffer().String()
		assert.Contains(t, output, "test message value")
	})

	t.Run("Print", func(t *testing.T) {
		logger := NewCheckLogger("test")
		logger.Print("test", " ", "message")

		output := logger.GetBuffer().String()
		assert.Contains(t, output, "test message")
	})

	t.Run("log format", func(t *testing.T) {
		logger := NewCheckLogger("test")
		logger.Print("test message")

		output := logger.GetBuffer().String()
		// Check log format includes timestamp
		lines := strings.Split(strings.TrimSpace(output), "\n")
		require.Len(t, lines, 1)

		// Standard log format: "2006/01/02 15:04:05 test message"
		parts := strings.SplitN(lines[0], " ", 3)
		require.Len(t, parts, 3)
		assert.Equal(t, "test message", strings.TrimSpace(parts[2]))
	})

	t.Run("multiple writes", func(t *testing.T) {
		logger := NewCheckLogger("test")
		logger.Print("first")
		logger.Print("second")

		output := logger.GetBuffer().String()
		assert.Contains(t, output, "first")
		assert.Contains(t, output, "second")
	})
}
