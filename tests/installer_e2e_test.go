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
	assert.Contains(t, outputStr, "Installation complete!  {\"status\": \"success\"}", "Installation should complete")

	// // check with docker if the container is running
	cmd = exec.Command(vmScriptPath, "curl", "https://localhost")
	stdout.Reset()
	stderr.Reset()
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	assert.NoError(t, err, "Failed to run curl")
	assert.Contains(t, stdout.String(), "infinity-metrics", "infinity-metrics container should be running")

	// // // check with docker if the container is running
	// cmd = exec.Command(vmScriptPath, "docker", "ps")
	// stdout.Reset()
	// stderr.Reset()
	// cmd.Stdout = &stdout
	// cmd.Stderr = &stderr
	// err = cmd.Run()
	// assert.NoError(t, err, "Failed to run docker ps command")
	// assert.Contains(t, stdout.String(), "infinity-metrics", "infinity-metrics container should be running")
}
