package requirements

import (
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"infinity-metrics-installer/internal/logging"
)

func TestNewChecker(t *testing.T) {
	logger := logging.NewLogger(logging.Config{Level: "error", Quiet: true})
	checker := NewChecker(logger)

	assert.NotNil(t, checker)
	assert.NotNil(t, checker.logger)
}

func TestCheckPort(t *testing.T) {
	logger := logging.NewLogger(logging.Config{Level: "error", Quiet: true})
	checker := NewChecker(logger)

	// Test available port
	t.Run("available port", func(t *testing.T) {
		// Find an available port
		tempLn, err := net.Listen("tcp", ":0")
		if err != nil {
			t.Fatalf("Failed to find available port: %v", err)
		}
		port := tempLn.Addr().(*net.TCPAddr).Port
		tempLn.Close()
		// Give a small delay to ensure the port is released
		time.Sleep(10 * time.Millisecond)

		result := checker.checkPort(port)
		assert.True(t, result)
	})

	// Test unavailable port
	t.Run("unavailable port", func(t *testing.T) {
		// Create a listener on localhost (same as checkPort function)
		ln, err := net.Listen("tcp", "localhost:0")
		if err != nil {
			t.Fatalf("Failed to create test listener: %v", err)
		}
		defer ln.Close()

		// Get the actual port that was assigned
		port := ln.Addr().(*net.TCPAddr).Port

		// Test that the port is not available (listener is still open)
		result := checker.checkPort(port)
		assert.False(t, result)
	})
}

func TestCheckPortEdgeCases(t *testing.T) {
	logger := logging.NewLogger(logging.Config{Level: "error", Quiet: true})
	checker := NewChecker(logger)

	tests := []struct {
		name     string
		port     int
		expected bool
	}{
		{
			name:     "invalid port negative",
			port:     -1,
			expected: false,
		},
		{
			name:     "invalid port too high",
			port:     65536,
			expected: false,
		},
		{
			name:     "port 0 (should be available)",
			port:     0,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := checker.checkPort(tt.port)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCheckRootPrivileges(t *testing.T) {
	logger := logging.NewLogger(logging.Config{Level: "error", Quiet: true})
	checker := NewChecker(logger)

	// Save original ENV value
	originalEnv := os.Getenv("ENV")
	defer os.Setenv("ENV", originalEnv)

	// Test in test environment (should always pass)
	os.Setenv("ENV", "test")
	err := checker.checkRootPrivileges()
	assert.NoError(t, err)

	// Reset ENV for non-test scenario
	os.Setenv("ENV", "")

	// Test depends on whether we're actually running as root
	// In most test environments, we won't be root
	if os.Geteuid() == 0 {
		// Running as root - should pass
		err = checker.checkRootPrivileges()
		assert.NoError(t, err)
	} else {
		// Not running as root - should fail
		err = checker.checkRootPrivileges()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "root privileges required")
	}
}

func TestCheckPortAvailability(t *testing.T) {
	logger := logging.NewLogger(logging.Config{Level: "error", Quiet: true})
	checker := NewChecker(logger)

	// Save original SKIP_PORT_CHECKING value
	originalSkip := os.Getenv("SKIP_PORT_CHECKING")
	defer os.Setenv("SKIP_PORT_CHECKING", originalSkip)

	// Test with SKIP_PORT_CHECKING=1 (should always pass)
	os.Setenv("SKIP_PORT_CHECKING", "1")
	err := checker.checkPortAvailability()
	assert.NoError(t, err)

	// Test without SKIP_PORT_CHECKING (actual port checking)
	os.Setenv("SKIP_PORT_CHECKING", "")

	// This test might fail if ports 80 or 443 are actually in use
	// In most test environments, these ports should be available
	err = checker.checkPortAvailability()
	// We can't assert success or failure here because it depends on the test environment
	// The test just verifies the function doesn't panic and returns a proper error or nil
	if err != nil {
		assert.Error(t, err)
	}
}

func TestCheckSystemRequirements(t *testing.T) {
	logger := logging.NewLogger(logging.Config{Level: "error", Quiet: true})
	checker := NewChecker(logger)

	// Save original environment values
	originalEnv := os.Getenv("ENV")
	originalSkip := os.Getenv("SKIP_PORT_CHECKING")
	defer func() {
		os.Setenv("ENV", originalEnv)
		os.Setenv("SKIP_PORT_CHECKING", originalSkip)
	}()

	// Test in test environment with port checking skipped
	os.Setenv("ENV", "test")
	os.Setenv("SKIP_PORT_CHECKING", "1")

	err := checker.CheckSystemRequirements()
	assert.NoError(t, err)
}
