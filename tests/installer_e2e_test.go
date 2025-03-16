package tests

import (
	"bytes"
	"crypto/tls"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestInstallation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Detect project root
	projectRoot, err := filepath.Abs("..")
	assert.NoError(t, err, "Failed to find project root")

	// Get binary path
	binaryPath := os.Getenv("BINARY_PATH")
	if binaryPath == "" {
		binaryPath = filepath.Join(projectRoot, "bin", "infinity-metrics")
		t.Logf("BINARY_PATH not set, using default path: %s", binaryPath)
	} else {
		t.Logf("BINARY_PATH set to: %s", binaryPath)
	}

	// Check if the binary exists
	assert.FileExists(t, binaryPath, "Binary should exist")

	// Check if running in GitHub Actions
	inGitHubActions := os.Getenv("GITHUB_ACTIONS") == "true"
	var cmd *exec.Cmd

	if inGitHubActions {
		// Run directly in GitHub Actions without VM
		t.Log("Running installer directly (GitHub Actions detected)")
		cmd = exec.Command(binaryPath, "install")
		// Provide input for CollectFromUser: Domain, AdminEmail, LicenseKey
		cmd.Stdin = bytes.NewBufferString("localhost\nadmin@localhost\nTEST-LICENSE-KEY\n")
	} else {
		// Use VM-based script locally
		t.Log("Running VM-based installation (not in GitHub Actions)")
		vmScriptPath := filepath.Join(projectRoot, "tests", "run_in_vm.sh")
		assert.FileExists(t, vmScriptPath, "VM script should exist")
		cmd = exec.Command(vmScriptPath, "--binary="+binaryPath, "--args=install", "--keep-vm")
		cmd.Env = append(os.Environ(), "DEBUG=1", "LICENSE_KEY=TEST-LICENSE-KEY")
	}

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	t.Log("Starting installation test...")
	err = cmd.Start()
	assert.NoError(t, err, "Failed to start installation")

	// Wait for completion with a timeout
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	timeout := 2 * time.Minute
	if inGitHubActions {
		timeout = 5 * time.Minute // Longer timeout for CI with Docker setup
	}
	select {
	case err = <-done:
		if err != nil {
			t.Logf("Command stdout:\n%s", stdout.String())
			t.Logf("Command stderr:\n%s", stderr.String())
			assert.NoError(t, err, "Installation should run successfully")
		}
	case <-time.After(timeout):
		cmd.Process.Kill()
		t.Logf("Command stdout:\n%s", stdout.String())
		t.Logf("Command stderr:\n%s", stderr.String())
		t.Fatalf("Installation timed out after %v", timeout)
	}

	// Log the captured output
	outputStr := stdout.String()
	errorStr := stderr.String()
	if outputStr != "" {
		t.Logf("Installer Output:\n%s", outputStr)
	}
	if errorStr != "" {
		t.Logf("Installer Errors:\n%s", errorStr)
	}

	// Assert installer completed without errors
	assert.NotContains(t, outputStr, "[ERROR]", "Installer should not return errors")
	assert.Contains(t, outputStr, "Installation completed successfully", "Installation should complete")

	// Test service access (only in GitHub Actions)
	if inGitHubActions {
		t.Log("Testing HTTPS access with retries...")
		url := "https://localhost:8443"
		client := &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		}

		var resp *http.Response
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

		assert.NoError(t, err, "HTTPS request to service should succeed")
		assert.NotNil(t, resp, "HTTPS response should not be nil")
		defer resp.Body.Close()
		assert.Contains(t, []int{http.StatusOK, http.StatusFound}, resp.StatusCode, "Service should return 200 OK or 302 Found")
		t.Logf("Successfully pinged service at %s, got %d", url, resp.StatusCode)
	}
}
