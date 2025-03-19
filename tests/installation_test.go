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
	projectRoot, err := filepath.Abs("..")
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
	licenseKey := os.Getenv("LICENSE_KEY")
	if licenseKey == "" {
		licenseKey = "TEST-LICENSE-KEY"
	}
	config.StdinInput = fmt.Sprintf(
		"localhost\n"+
			"admin@localhost\n"+
			"%s\n"+
			"\n",
		licenseKey)
	config.Debug = os.Getenv("DEBUG") == "1"
	config.Timeout = 10 * time.Minute // Increased timeout

	runner := testrunner.NewTestRunner(config)
	os.Setenv("KEEP_VM", "1")
	defer os.Setenv("KEEP_VM", os.Getenv("KEEP_VM"))

	err = runner.Run()
	outputStr := runner.Stdout()
	errorStr := runner.Stderr()
	t.Logf("Installer Output:\n%s", outputStr)
	if errorStr != "" {
		t.Logf("Installer Errors:\n%s", errorStr)
	}

	// Robust assertions
	require.NoError(t, err, "Installation should complete without error")
	// assert.NotContains(t, outputStr, "ERROR", "Installer should not return errors")
	// assert.Contains(t, outputStr, "✔ Configuration collected from user", "Configuration should be collected")
	// assert.Contains(t, outputStr, "✔ Installation completed in", "Installation should complete successfully")
	// assert.Contains(t, outputStr, "Access your dashboard at https://localhost", "Config should apply domain")

	t.Log("Testing service availability...")
	testServiceAvailability(t, isRunningInCI(), config.VMName)

	if os.Getenv("KEEP_VM") != "1" {
		cleanupTestEnvironment(t, config.VMName)
	}
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
	cmd = exec.Command("docker", "system", "prune", "-f")
	if err := cmd.Run(); err != nil {
		t.Logf("Failed to prune Docker: %v", err)
	}
}
