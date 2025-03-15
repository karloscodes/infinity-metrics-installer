package tests

import (
	"os"
	"testing"

	"infinity-metrics-installer/internal/config"
	"infinity-metrics-installer/internal/logging"
)

// TestConfigCollection is used by the e2e tests to collect configuration
// without running the full installation. It only runs when INFINITY_TEST_MODE=true.
func TestConfigCollection(t *testing.T) {
	// Only run when in test mode
	if os.Getenv("INFINITY_TEST_MODE") != "true" {
		t.Skip("Not in test mode")
	}

	// Create logger
	logger := logging.NewLogger(logging.Config{
		Level: "info",
	})

	// Create config
	cfg := config.NewConfig(config.WithLogger(logger))

	// Collect configuration from stdin
	err := cfg.CollectFromUser()
	if err != nil {
		t.Fatalf("Failed to collect configuration: %v", err)
	}

	// Get the config dir from environment or use a default
	configDir := os.Getenv("INFINITY_CONFIG_DIR")
	if configDir == "" {
		configDir = os.TempDir()
	}

	// Save the configuration to a file
	err = cfg.SaveToFile(configDir + "/.env")
	if err != nil {
		t.Fatalf("Failed to save configuration: %v", err)
	}

	t.Log("Configuration collected successfully")
}
