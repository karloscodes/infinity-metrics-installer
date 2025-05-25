package tests

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"infinity-metrics-installer/internal/pkg/testrunner"
)

// TestInstallation tests the infinity-metrics install command
func TestInstallation(t *testing.T) {
	// Ensure binary exists
	binaryPath := os.Getenv("BINARY_PATH")
	if binaryPath == "" {
		binaryPath = "../bin/infinity-metrics"
	}

	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		t.Fatalf("Binary not found at %s", binaryPath)
	}
	
	// Check for VM provider from environment
	vmProvider := os.Getenv("VM_PROVIDER")
	if vmProvider != "" {
		t.Logf("Using VM provider from environment: %s", vmProvider)
	}

	// Configure test runner
	config := testrunner.DefaultConfig()
	config.BinaryPath = binaryPath
	config.Args = []string{"install"}

	// Test input string (auto-answer prompts)
	config.StdinInput = strings.Join([]string{
		"test.example.com",  // Domain
		"admin@example.com", // Admin email
		"test-license-key",  // License key
		// Password is provided via env var
		"y", // Confirm settings
	}, "\n")

	// Set environment variables for test
	config.EnvVars = map[string]string{
		"ADMIN_PASSWORD":      "securepassword123",
		"ENV":                 "test",
		"SKIP_DNS_VALIDATION": "1", // Skip DNS validation
	}

	// Use VM mode for testing

	// For debugging during development
	if os.Getenv("DEBUG") == "1" {
		config.Debug = true
	}

	// If specified, keep the VM for debugging
	if os.Getenv("KEEP_VM") == "1" {
		config.VMName = "infinity-metrics-test"
	}

	t.Log("Running installation test...")

	// Create and run the test runner
	runner := testrunner.NewTestRunner(config)
	err := runner.Run()

	// Check output
	stdout := runner.Stdout()
	t.Log("Installer Output:")
	t.Log(stdout)

	// Verify installation succeeded
	require.NoError(t, err, "Installation should complete without error")

	// Check for success message
	assert.Contains(t, stdout, "Installation completed", "Output should confirm successful installation")
}

func isRunningInCI() bool {
	return os.Getenv("GITHUB_ACTIONS") == "true" || os.Getenv("GITHUB_RUN_NUMBER") != ""
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
		cmd := exec.Command("multipass", "exec", vmName, "--",
			"curl", "-k", "-s", "-o", "/dev/null", "-w", "%{http_code}", url)
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
		logCmd := exec.Command("multipass", "exec", vmName, "--",
			"sudo", "cat", "/opt/infinity-metrics/logs/infinity-metrics.log")
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
}
