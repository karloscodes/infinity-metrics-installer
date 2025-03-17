package tests

import (
	"crypto/tls"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"infinity-metrics-installer/internal/pkg/testrunner"
)

func TestInstallation(t *testing.T) {
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
	config.StdinInput = "localhost\nadmin@localhost\nTEST-LICENSE-KEY\n"
	config.Debug = os.Getenv("DEBUG") == "1"
	config.Timeout = 5 * time.Minute

	// Create and run the test runner
	runner := testrunner.NewTestRunner(config)
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

	// Test service access (only in GitHub Actions)
	if os.Getenv("GITHUB_ACTIONS") == "true" || os.Getenv("GITHUB_RUN_NUMBER") != "" {
		t.Log("Testing HTTPS access with retries...")
		testServiceAvailability(t)
	} else {
		t.Log("Skipping service availability test in local environment")
	}
}

// testServiceAvailability tests if the installed service is responding
func testServiceAvailability(t *testing.T) {
	url := "https://localhost:8443"
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
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
	assert.Contains(t, []int{http.StatusOK, http.StatusFound}, resp.StatusCode,
		"Service should return 200 OK or 302 Found")
	t.Logf("Successfully pinged service at %s, got %d", url, resp.StatusCode)
}
