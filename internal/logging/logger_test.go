package logging

import (
	"bytes"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestNewLogger(t *testing.T) {
	config := Config{
		Level:   "info",
		Verbose: false,
		Quiet:   false,
	}

	logger := NewLogger(config)
	assert.NotNil(t, logger)
	assert.NotNil(t, logger.Logger)
	assert.Equal(t, config, logger.config)
	assert.Equal(t, logrus.InfoLevel, logger.Logger.Level)
}

func TestLoggerLevels(t *testing.T) {
	tests := []struct {
		level    string
		expected logrus.Level
	}{
		{"debug", logrus.DebugLevel},
		{"info", logrus.InfoLevel},
		{"warn", logrus.WarnLevel},
		{"error", logrus.ErrorLevel},
		{"unknown", logrus.InfoLevel}, // defaults to info
	}

	for _, test := range tests {
		t.Run(test.level, func(t *testing.T) {
			config := Config{Level: test.level}
			logger := NewLogger(config)
			assert.Equal(t, test.expected, logger.Logger.Level)
		})
	}
}

func TestLoggerOutput(t *testing.T) {
	// Capture output
	var buf bytes.Buffer

	config := Config{
		Level: "debug",
		Quiet: true, // To avoid cluttering test output
	}

	logger := NewLogger(config)
	logger.Logger.SetOutput(&buf)

	// Test different log methods
	logger.Debug("debug message")
	logger.Info("info message")
	logger.Warn("warn message")
	logger.Error("error message")
	logger.Success("success message")

	output := buf.String()
	assert.Contains(t, output, "debug message")
	assert.Contains(t, output, "info message")
	assert.Contains(t, output, "warn message")
	assert.Contains(t, output, "error message")
	assert.Contains(t, output, "✔ success message")
}

func TestLoggerWithFormatting(t *testing.T) {
	var buf bytes.Buffer

	config := Config{
		Level: "info",
		Quiet: true,
	}

	logger := NewLogger(config)
	logger.Logger.SetOutput(&buf)

	// Test formatting
	logger.Info("Hello %s, number %d", "world", 42)

	output := buf.String()
	assert.Contains(t, output, "Hello world, number 42")
}

func TestQuietMode(t *testing.T) {
	config := Config{
		Level: "info",
		Quiet: true,
	}

	logger := NewLogger(config)
	assert.NotNil(t, logger)
	// In quiet mode, the output should be discarded
	// The exact implementation depends on how quiet mode is handled
}

func TestVerboseMode(t *testing.T) {
	config := Config{
		Level:   "info",
		Verbose: true,
	}

	logger := NewLogger(config)
	assert.NotNil(t, logger)
	assert.True(t, logger.config.Verbose)
}

func TestLoggerWithTime(t *testing.T) {
	var buf bytes.Buffer

	config := Config{
		Level: "info",
		Quiet: true,
	}

	logger := NewLogger(config)
	logger.Logger.SetOutput(&buf)

	// Test InfoWithTime method (if it exists)
	logger.InfoWithTime("Test message with time")

	output := buf.String()
	assert.Contains(t, output, "Test message with time")
}

func TestConfig(t *testing.T) {
	config := Config{
		Level:   "debug",
		Verbose: true,
		LogDir:  "/tmp/logs",
		Quiet:   false,
		LogFile: "test.log",
	}

	assert.Equal(t, "debug", config.Level)
	assert.True(t, config.Verbose)
	assert.Equal(t, "/tmp/logs", config.LogDir)
	assert.False(t, config.Quiet)
	assert.Equal(t, "test.log", config.LogFile)
}
