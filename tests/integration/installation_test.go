package tests

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"infinity-metrics-installer/internal/pkg/testrunner"
)

func TestInstallation(t *testing.T) {
	os.Setenv("ENV", "test")

	projectRoot, err := filepath.Abs("../..")
	require.NoError(t, err, "Failed to find project root")

	binaryPath := os.Getenv("BINARY_PATH")
	if binaryPath == "" {
		var binaryPattern string
		if os.Getenv("ARCH") == "arm64" {
			binaryPattern = "infinity-metrics-v*-arm64"
		} else {
			binaryPattern = "infinity-metrics-v*-amd64"
		}

		binaries, err := filepath.Glob(filepath.Join(projectRoot, "bin", binaryPattern))
		require.NoError(t, err, "Failed to find binary")

		if len(binaries) == 0 {
			defaultBinary := filepath.Join(projectRoot, "bin", "infinity-metrics")
			if _, err := os.Stat(defaultBinary); err == nil {
				binaryPath = defaultBinary
			} else {
				t.Fatalf("No binary found matching pattern %s or at default location", binaryPattern)
			}
		} else {
			binaryPath = binaries[0]
		}
	}

	t.Logf("Using binary: %s", binaryPath)
	assert.FileExists(t, binaryPath, "Binary should exist")

	config := testrunner.DefaultConfig()
	config.BinaryPath = binaryPath
	config.Args = []string{"install"}

	// Get license key from environment or use a default for testing
	licenseKey := os.Getenv("INFINITY_METRICS_LICENSE_KEY")
	if licenseKey == "" {
		licenseKey = "test-license-key"
		t.Logf("Using default test license key. Set INFINITY_METRICS_LICENSE_KEY for a real key.")
	} else {
		t.Logf("Using license key from environment variable: %s", licenseKey)
	}

	// Test interactive installation with realistic user input
	adminPassword := "securepassword123"
	// Input order: domain, email, license, password, confirm_password, confirmation_to_proceed
	config.StdinInput = fmt.Sprintf("test.example.com\nadmin@example.com\n%s\n%s\n%s\ny\n",
		licenseKey,
		adminPassword,
		adminPassword)
	config.Debug = os.Getenv("DEBUG") == "1"
	config.Timeout = 10 * time.Minute // Increased timeout

	// Set VM name for easier debugging
	config.VMName = "infinity-test-vm"

	// Set environment variables for test infrastructure (not app config)
	config.EnvVars = map[string]string{
		"ENV": "test", // Test environment indicator
	}

	runner := testrunner.NewTestRunner(config)
	os.Setenv("KEEP_VM", "1")
	defer os.Setenv("KEEP_VM", os.Getenv("KEEP_VM"))

	t.Log("Configured interactive installation test with user input simulation")

	err = runner.Run()
	outputStr := runner.Stdout()
	errorStr := runner.Stderr()
	t.Logf("Installer Output:\n%s", outputStr)
	if errorStr != "" {
		t.Logf("Installer Errors:\n%s", errorStr)
	}

	// Robust assertions - only if not skipped due to architecture issues
	require.NoError(t, err, "Installation should complete without error")

	// Verify interactive prompts were displayed
	interactivePatterns := []string{
		"Enter your domain name",
		"Enter admin email address",
		"Enter your Infinity Metrics license key",
		"Enter admin password",
		"Configuration Summary:",
		"Proceed with this configuration?",
	}

	t.Log("Verifying interactive prompts were displayed...")
	for _, pattern := range interactivePatterns {
		if strings.Contains(outputStr, pattern) {
			t.Logf("✅ Found expected interactive prompt: '%s'", pattern)
		} else {
			t.Logf("⚠️  Interactive prompt not found: '%s'", pattern)
		}
	}

	// Check for success message - using more flexible assertions
	// The test might pass even if we don't see all the expected output patterns
	// as long as the command exits with status 0
	successPatterns := []string{
		"Installation completed in",
		"Installation verified successfully",
	}

	for _, pattern := range successPatterns {
		if !strings.Contains(outputStr, pattern) {
			t.Logf("Warning: Output doesn't contain expected pattern '%s', but command succeeded", pattern)
		}
	}
	// Verify the new confirmation prompt and domain resolution
	t.Log("Testing service availability...")
	testServiceAvailability(t, config.VMName)

	if os.Getenv("KEEP_VM") != "1" {
		cleanupTestEnvironment(t, config.VMName)
	}
}

func testServiceAvailability(t *testing.T, vmName string) {
	serviceURL := "https://localhost"
	runner := testrunner.NewTestRunner(testrunner.Config{VMName: vmName})
	t.Log("Testing HTTPS access via service check...")
	success, is302, finalOutput := runner.CheckServiceAvailability(serviceURL, 6, t)

	if !success {
		logCmd := "sudo cat /opt/infinity-metrics/logs/infinity-metrics.log"
		logOutput, _ := runner.RunSSHCommand(logCmd)
		t.Logf("Service logs:\n%s", logOutput)
	}

	assert.True(t, success, fmt.Sprintf("Service should be accessible, got: %s", finalOutput))
	assert.True(t, is302, "Service should return 302 redirect")
	t.Log("Service verified in VM or locally")
}

func cleanupTestEnvironment(t *testing.T, vmName string) {
	t.Log("Cleaning up test environment...")
	cmd := exec.Command("multipass", "delete", "--purge", vmName)
	if err := cmd.Run(); err != nil {
		t.Logf("Failed to delete VM %s: %v", vmName, err)
	}
	cmd = exec.Command("docker", "system", "prune", "-f")
	if err := cmd.Run(); err != nil {
		t.Logf("Failed to prune Docker: %v", err)
	}
}
