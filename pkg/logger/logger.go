package logger

import (
	"bytes"
	"log"
)

// CheckLogger handles logging for individual check runs
type CheckLogger struct {
	buf    *bytes.Buffer
	logger *log.Logger
	id     string
}

// NewCheckLogger creates a new logger for a check run
func NewCheckLogger(id string) *CheckLogger {
	buf := &bytes.Buffer{}

	return &CheckLogger{
		buf:    buf,
		logger: log.New(buf, "", log.LstdFlags),
		id:     id,
	}
}

// Printf logs a formatted message
func (l *CheckLogger) Printf(format string, v ...interface{}) {
	l.logger.Printf(format, v...)
}

// Print logs a message
func (l *CheckLogger) Print(v ...interface{}) {
	l.logger.Print(v...)
}

// GetID returns the check run ID
func (l *CheckLogger) GetID() string {
	return l.id
}

// GetBuffer returns the underlying buffer
func (l *CheckLogger) GetBuffer() *bytes.Buffer {
	return l.buf
}
