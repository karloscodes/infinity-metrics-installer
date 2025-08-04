package tests

import (
	"crypto/tls"
	"fmt"
	"net/http"
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

func isRunningInCI() bool {
	return os.Getenv("GITHUB_ACTIONS") == "true" || os.Getenv("GITHUB_RUN_NUMBER") != ""
}

func TestInstallation(t *testing.T) {
	os.Setenv("ENV", "test")

	projectRoot, err := filepath.Abs("../..")
	require.NoError(t, err, "Failed to find project root")

	binaryPath := os.Getenv("BINARY_PATH")
	if binaryPath == "" {
		var binaryPattern string
		var binaryPatternOld string
		if os.Getenv("ARCH") == "arm64" {
			binaryPattern = "infinity-metrics-installer-v*-arm64"
			binaryPatternOld = "infinity-metrics-v*-arm64"
		} else {
			binaryPattern = "infinity-metrics-installer-v*-amd64"
			binaryPatternOld = "infinity-metrics-v*-amd64"
		}

		// Try new naming pattern first
		binaries, err := filepath.Glob(filepath.Join(projectRoot, "bin", binaryPattern))
		require.NoError(t, err, "Failed to find binary")

		// If new pattern not found, try old pattern
		if len(binaries) == 0 {
			binaries, err = filepath.Glob(filepath.Join(projectRoot, "bin", binaryPatternOld))
			require.NoError(t, err, "Failed to find binary")
		}

		if len(binaries) == 0 {
			defaultBinary := filepath.Join(projectRoot, "bin", "infinity-metrics")
			if _, err := os.Stat(defaultBinary); err == nil {
				binaryPath = defaultBinary
			} else {
				t.Fatalf("No binary found matching pattern %s or %s or at default location", binaryPattern, binaryPatternOld)
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

	adminPassword := "SecurePassword123!"
	if isRunningInCI() {
		os.Setenv("ADMIN_PASSWORD", adminPassword)
		t.Log("Set ADMIN_PASSWORD in CI environment")
		// Use localhost as domain in CI
		config.StdinInput = fmt.Sprintf("localhost\nadmin@example.com\n%s\n\n", licenseKey)
	} else {
		// If ADMIN_PASSWORD is set, do NOT provide password/confirmation in stdin
		config.EnvVars["ADMIN_PASSWORD"] = adminPassword
		// Use localhost as domain for local tests as well
		config.StdinInput = fmt.Sprintf("localhost\nadmin@example.com\n%s\ny\n",
			licenseKey)
	}
	config.Debug = os.Getenv("DEBUG") == "1"
	config.Timeout = 10 * time.Minute // Increased timeout
	config.VMName = "infinity-test-vm"
	config.EnvVars["ENV"] = "test"
	config.EnvVars["SKIP_PORT_CHECKING"] = "1"

	runner := testrunner.NewTestRunner(config)
	os.Setenv("KEEP_VM", "1")
	defer os.Setenv("KEEP_VM", os.Getenv("KEEP_VM"))

	t.Log("Configured interactive installation test with user input simulation")

	err = runner.Run()
	outputStr := runner.Stdout()
	errorStr := runner.Stderr()

	// Robust assertions - only if not skipped due to architecture issues
	require.NoError(t, err, "Installation should complete without error")

	// Only print installer output if the test fails
	if t.Failed() {
		t.Logf("Installer Output (on failure):\n%s", outputStr)
		if errorStr != "" {
			t.Logf("Installer Errors:\n%s", errorStr)
		}
	}

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

	successPatterns := []string{
		"Installation completed in",
		"Installation verified successfully",
	}

	for _, pattern := range successPatterns {
		assert.Contains(t, outputStr, pattern, "Output should contain success pattern '%s'", pattern)
		if !strings.Contains(outputStr, pattern) {
			t.Logf("Warning: Output doesn't contain expected pattern '%s', but command succeeded", pattern)
		}
	}
	t.Log("Testing service availability...")
	testServiceAvailability(t, isRunningInCI(), config.VMName)

	if os.Getenv("KEEP_VM") != "1" {
		cleanupTestEnvironment(t, config.VMName)
	}

	// Optionally, print the installer output as a summary at the end
	// t.Logf("Installer Output (summary):\n%s", outputStr)
}

func testServiceAvailability(t *testing.T, isCI bool, vmName string) {
	serviceURL := "https://localhost"
	if isCI {
		t.Log("Testing HTTPS access with direct HTTP client...")
		testDirectServiceAccess(t, serviceURL)
	} else {
		t.Log("Testing HTTPS access via VM curl command...")
		testVMServiceAccess(t, vmName, serviceURL)
	}
}

func testDirectServiceAccess(t *testing.T, url string) {
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	var resp *http.Response
	var err error
	for i := 0; i < 6; i++ {
		resp, err = client.Get(url)
		if err == nil && (resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusFound) {
			break
		}
		if resp != nil {
			resp.Body.Close()
		}
		t.Logf("Waiting for service (attempt %d/6)...", i+1)
		time.Sleep(5 * time.Second)
	}

	require.NoError(t, err, "HTTPS request should succeed")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusFound, resp.StatusCode, "Service should return 302 Found")
	t.Logf("Service responded with %d, Location: %s", resp.StatusCode, resp.Header.Get("Location"))
}

func testVMServiceAccess(t *testing.T, vmName string, url string) {
	var success bool
	var finalOutput string
	var is302 bool

	for i := 0; i < 6; i++ {
		cmd := exec.Command("multipass", "exec", vmName, "--", "curl", "-k", "-s", "-o", "/dev/null", "-w", "%{http_code}", url)
		output, err := cmd.CombinedOutput()
		outputStr := strings.TrimSpace(string(output))
		finalOutput = outputStr

		t.Logf("Curl attempt %d/6, result: %s, error: %v", i+1, outputStr, err)
		if err == nil && outputStr == "302" {
			success = true
			is302 = true
			break
		} else if err == nil && outputStr == "200" {
			success = true
		}
		time.Sleep(5 * time.Second)
	}

	if !success {
		logCmd := exec.Command("multipass", "exec", vmName, "--", "sudo", "cat", "/opt/infinity-metrics/logs/infinity-metrics.log")
		logOutput, _ := logCmd.CombinedOutput()
		t.Logf("Service logs:\n%s", string(logOutput))
	}

	assert.True(t, success, fmt.Sprintf("Service should be accessible, got: %s", finalOutput))
	assert.True(t, is302, "Service should return 302 redirect")
	t.Log("Service verified in VM")
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
