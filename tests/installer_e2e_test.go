package tests

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestVMInstallation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping VM-based test in short mode")
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

	// Path to the run_in_vm.sh script
	vmScriptPath := filepath.Join(projectRoot, "tests", "run_in_vm.sh")
	assert.FileExists(t, vmScriptPath, "VM script should exist")

	// Run the script with the install command
	cmd := exec.Command(vmScriptPath, "--binary="+binaryPath, "--args=install", "--keep-vm")
	cmd.Env = append(os.Environ(), "DEBUG=1") // Enable debug output in script

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	t.Log("Starting VM installation test...")
	err = cmd.Start()
	assert.NoError(t, err, "Failed to start VM installation script")

	// Wait for completion with a timeout
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err = <-done:
		if err != nil {
			t.Logf("Script stdout:\n%s", stdout.String())
			t.Logf("Script stderr:\n%s", stderr.String())
			assert.NoError(t, err, "VM installation script should run successfully")
		}
	case <-time.After(2 * time.Minute):
		cmd.Process.Kill()
		t.Logf("Script stdout:\n%s", stdout.String())
		t.Logf("Script stderr:\n%s", stderr.String())
		t.Fatalf("VM installation script timed out after 10 minutes")
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

	// // Assert installer completed without errors
	assert.NotContains(t, outputStr, "[ERROR]", "Installer should not return errors")
	assert.Contains(t, outputStr, "Installation completed successfully", "Installation should complete")

	// t.Log("Testing HTTPS access with retries...")

	// var curlSuccess bool
	// var lastStdout, lastStderr string

	// // Try up to 10 times with exponential backoff
	// for attempt := 1; attempt <= 5; attempt++ {
	// 	t.Logf("Curl attempt %d of 5", attempt)

	// 	domainCheckCmd := exec.Command("multipass", "exec", "infinity-test-vm", "--", "curl", "-k", "-s", "-v",
	// 		"https://localhost")

	// 	stdout.Reset()
	// 	stderr.Reset()
	// 	domainCheckCmd.Stdout = &stdout
	// 	domainCheckCmd.Stderr = &stderr

	// 	err = domainCheckCmd.Run()
	// 	lastStdout = stdout.String()
	// 	lastStderr = stderr.String()

	// 	if err == nil && (strings.Contains(lastStdout, "HTTP/2 302") || strings.Contains(lastStderr, "HTTP/2 302")) {
	// 		t.Logf("Curl succeeded on attempt %d", attempt)
	// 		curlSuccess = true
	// 		break
	// 	}

	// 	// Log the failure and wait before retry
	// 	t.Logf("Curl attempt %d failed. Stdout: %s", attempt, lastStdout)
	// 	t.Logf("Stderr: %s", lastStderr)

	// 	// Exponential backoff - wait longer between successive attempts
	// 	waitTime := time.Duration(math.Pow(2, float64(attempt-1))) * time.Second
	// 	if waitTime > 30*time.Second {
	// 		waitTime = 30 * time.Second // Cap at 30 seconds
	// 	}

	// 	t.Logf("Waiting %v before next attempt", waitTime)
	// 	time.Sleep(waitTime)
	// }

	// // Final assertion
	// if !curlSuccess {
	// 	t.Logf("All curl attempts failed. Last stdout: %s", lastStdout)
	// 	t.Logf("Last stderr: %s", lastStderr)
	// 	assert.Fail(t, "Failed to curl the configured domain after 5 attempts")
	// } else {
	// 	assert.True(t, curlSuccess, "Service should be accessible via HTTPS")
	// 	assert.True(t, strings.Contains(lastStdout, "HTTP/2 302") || strings.Contains(lastStderr, "HTTP/2 302"),
	// 		"Service should return HTTP/2 302 status code")
	// }
}
