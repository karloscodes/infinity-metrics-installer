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

func TestInstallation(t *testing.T) {
	os.Setenv("ENV", "test")
	// Detect project root
	projectRoot, err := filepath.Abs("..")
	require.NoError(t, err, "Failed to find project root")

	// Get binary path
	binaryPath := os.Getenv("BINARY_PATH")
	if binaryPath == "" {
		// Find the appropriate binary based on architecture
		var binaryPattern string
		if os.Getenv("ARCH") == "arm64" {
			binaryPattern = "infinity-metrics-v*-arm64"
		} else {
			binaryPattern = "infinity-metrics-v*-amd64"
		}

		// Find the binary using glob pattern
		binaries, err := filepath.Glob(filepath.Join(projectRoot, "bin", binaryPattern))
		require.NoError(t, err, "Failed to find binary")

		if len(binaries) == 0 {
			// If not found, try the default binary name
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

	// Check if the binary exists
	assert.FileExists(t, binaryPath, "Binary should exist")

	// Create test runner config for direct execution
	config := testrunner.DefaultConfig()
	config.BinaryPath = binaryPath
	config.Args = []string{"install"}
	licenseKey := os.Getenv("LICENSE_KEY")
	if licenseKey == "" {
		licenseKey = "TEST-LICENSE-KEY" // Fallback value if environment variable is not set
	}
	config.StdinInput = fmt.Sprintf("localhost\nadmin@localhost\n%s\n", licenseKey)
	config.Debug = os.Getenv("DEBUG") == "1"
	config.Timeout = 5 * time.Minute

	// Create and run the test runner
	runner := testrunner.NewTestRunner(config)

	// Force VM to be kept after the test run for service testing
	oldKeepVM := os.Getenv("KEEP_VM")
	os.Setenv("KEEP_VM", "1")             // Set this temporarily
	defer os.Setenv("KEEP_VM", oldKeepVM) // Restore original value when done

	err = runner.Run()

	// Check outputs regardless of success/failure
	outputStr := runner.Stdout()
	errorStr := runner.Stderr()
	t.Logf("Installer Output:\n%s", outputStr)
	if errorStr != "" {
		t.Logf("Installer Errors:\n%s", errorStr)
	}

	// Assert installer ran successfully
	require.NoError(t, err, "Installation should complete without error")
	assert.NotContains(t, outputStr, "[ERROR]", "Installer should not return errors")
	assert.Contains(t, outputStr, "Installation completed successfully", "Installation should complete")

	// Test service access in both environments
	t.Log("Testing service availability...")
	testServiceAvailability(t, isRunningInCI(), config.VMName)
}

// isRunningInCI determines if we're running in CI environment
func isRunningInCI() bool {
	return os.Getenv("GITHUB_ACTIONS") == "true" || os.Getenv("GITHUB_RUN_NUMBER") != ""
}

// testServiceAvailability tests if the installed service is responding
func testServiceAvailability(t *testing.T, isCI bool, vmName string) {
	// We'll use a different approach based on the environment
	serviceUrl := "https://localhost"

	if isCI {
		// In CI, we can make direct HTTP requests
		t.Log("Testing HTTPS access with direct HTTP client...")
		testDirectServiceAccess(t, serviceUrl)
	} else {
		// In local environment with VM, we need to execute curl inside the VM
		t.Log("Testing HTTPS access via VM curl command...")
		testVMServiceAccess(t, vmName, serviceUrl)
	}
}

// testDirectServiceAccess tests the service in CI environment with direct HTTP requests
func testDirectServiceAccess(t *testing.T, url string) {
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		// Don't follow redirects so we can check for 302 status
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	var resp *http.Response
	var err error

	for i := 0; i < 12; i++ { // 12 * 5s = 60s
		resp, err = client.Get(url)
		if err == nil && (resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusFound) {
			break
		}
		if resp != nil {
			resp.Body.Close()
		}
		t.Logf("Waiting for service to respond (attempt %d/12)...", i+1)
		time.Sleep(5 * time.Second)
	}

	require.NoError(t, err, "HTTPS request to service should succeed")
	require.NotNil(t, resp, "HTTPS response should not be nil")
	defer resp.Body.Close()

	// Check for 302 Found specifically
	assert.Equal(t, http.StatusFound, resp.StatusCode, "Service should return 302 Found")
	t.Logf("Successfully pinged service at %s, got %d", url, resp.StatusCode)

	// Log redirect location
	location := resp.Header.Get("Location")
	t.Logf("Redirect location: %s", location)
}

// testVMServiceAccess tests the service in local environment via VM curl command
func testVMServiceAccess(t *testing.T, vmName string, url string) {
	// We'll use curl inside the VM to check the service
	var success bool
	var finalOutput string
	var is302 bool

	// Try several times with delay
	for i := 0; i < 12; i++ { // 12 * 5s = 60s
		// Form the curl command to execute inside the VM
		// Note: using -k to ignore SSL certificate verification
		cmd := exec.Command("multipass", "exec", vmName, "--",
			"curl", "-k", "-s", "-o", "/dev/null", "-w", "%{http_code}", url)

		output, err := cmd.CombinedOutput()
		outputStr := strings.TrimSpace(string(output))
		finalOutput = outputStr

		t.Logf("Curl attempt %d/12, result: %s, error: %v", i+1, outputStr, err)

		// Check if we got a 302 status code specifically
		if err == nil && outputStr == "302" {
			success = true
			is302 = true
			break
		} else if err == nil && outputStr == "200" {
			// 200 OK is also acceptable but not preferred
			success = true
		}

		// Sleep before trying again
		time.Sleep(5 * time.Second)

		// After a few attempts, let's try an alternate port
		if i == 5 {
			// Try port 443 (standard HTTPS) in case 8443 isn't the right one
			url = "https://localhost:443"
			t.Logf("Switching to alternative URL: %s", url)
		}
	}

	// Check VM service logs if we couldn't access it
	if !success {
		t.Log("Service not responding, checking logs...")
		logCmd := exec.Command("multipass", "exec", vmName, "--",
			"sudo", "cat", "/opt/infinity-metrics/logs/infinity-metrics.log")

		logOutput, _ := logCmd.CombinedOutput()
		t.Logf("Service logs:\n%s", string(logOutput))

		// Also check if the service process is running
		psCmd := exec.Command("multipass", "exec", vmName, "--",
			"ps", "aux", "|", "grep", "infinity")

		psOutput, _ := psCmd.CombinedOutput()
		t.Logf("Process info:\n%s", string(psOutput))
	}

	// Assert on the test results
	assert.True(t, success, fmt.Sprintf("Service should be accessible, got: %s", finalOutput))
	assert.True(t, is302, "Service should return a 302 redirect status code")
	t.Logf("Successfully verified service is running in VM")
}
