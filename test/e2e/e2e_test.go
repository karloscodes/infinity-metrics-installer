package e2e

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"infinity-metrics-installer/internal/pkg/testrunner"
)

func TestE2EInstallation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	// Get the binary path
	workDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}

	// Go up two levels to reach the root
	projectRoot := filepath.Join(workDir, "..", "..")
	binaryPath := filepath.Join(projectRoot, "bin", "infinity-metrics")

	// Check if binary exists
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		t.Fatalf("Binary not found at %s. Run 'make build' first.", binaryPath)
	}

	// Setup test configuration
	config := testrunner.DefaultConfig()
	config.BinaryPath = binaryPath
	config.Args = []string{"install"}
	config.Timeout = 15 * time.Minute // Give extra time for e2e tests
	config.VMName = "infinity-e2e-test"

	// Load test input from file
	testInputPath := filepath.Join(workDir, "test_input.txt")
	testInputBytes, err := os.ReadFile(testInputPath)
	if err != nil {
		t.Fatalf("Failed to read test input file: %v", err)
	}
	config.StdinInput = string(testInputBytes)

	// Set environment variables for test
	config.EnvVars = map[string]string{
		"ADMIN_PASSWORD":      "securepassword123",
		"SKIP_DNS_VALIDATION": "1",
		"ENV":                 "test",
		"NONINTERACTIVE":      "1",
		"SKIP_DOCKER_PULL":    "1",
	}

	config.Debug = true // Enable debug logging for e2e tests

	t.Logf("Running E2E installation test...")
	runner := testrunner.NewTestRunner(config)

	// Keep VM for inspection on failure
	os.Setenv("KEEP_VM", "1")
	defer os.Unsetenv("KEEP_VM")

	err = runner.Run()
	outputStr := runner.Stdout()
	errorStr := runner.Stderr()

	if outputStr != "" {
		t.Logf("Installation Output:\n%s", outputStr)
	}
	if errorStr != "" {
		t.Logf("Installation Errors:\n%s", errorStr)
	}

	if err != nil {
		t.Errorf("E2E installation test failed: %v", err)
		t.Logf("VM kept for inspection: %s", config.VMName)
	} else {
		t.Log("E2E installation test completed successfully")
		// Clean up VM on success
		os.Setenv("KEEP_VM", "0")
	}
}
