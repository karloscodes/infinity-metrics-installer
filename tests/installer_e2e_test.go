package tests

import (
	"bytes"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	case <-time.After(10 * time.Minute):
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

	// Assert installer completed without errors
	assert.NotContains(t, outputStr, "[ERROR] Installation failed", "Installer should not report a failure")
	assert.Contains(t, outputStr, "[SUCCESS] SQLite installed successfully", "SQLite should be installed")
	assert.Contains(t, outputStr, "[SUCCESS] Docker installed", "Docker should be installed")
	assert.Contains(t, outputStr, "[SUCCESS] Installation completed successfully", "Installation should complete")

	// Extract VM IP from multipass info infinity-test-vm
	cmd = exec.Command("multipass", "info", "infinity-test-vm")
	var ipOutput bytes.Buffer
	cmd.Stdout = &ipOutput
	err = cmd.Run()
	assert.NoError(t, err, "Failed to get VM info")
	vmInfo := ipOutput.String()
	t.Logf("VM Info:\n%s", vmInfo)

	vmIP := ""
	for _, line := range strings.Split(vmInfo, "\n") {
		if strings.Contains(line, "IPv4") {
			fields := strings.Fields(line)
			if len(fields) > 1 {
				vmIP = fields[1]
				break
			}
		}
	}
	assert.NotEmpty(t, vmIP, "VM IP should be found")
	t.Logf("VM IP extracted: %s", vmIP)

	// Ping the application via HTTP
	url := "http://" + vmIP + ":8080/_health"
	client := &http.Client{Timeout: 10 * time.Second}

	// Wait for the application to be ready (up to 60 seconds)
	var resp *http.Response
	for i := 0; i < 12; i++ { // 12 * 5s = 60s
		resp, err = client.Get(url)
		if err == nil && resp.StatusCode == http.StatusOK {
			break
		}
		if resp != nil {
			resp.Body.Close()
		}
		t.Logf("Waiting for application to respond (attempt %d/12)...", i+1)
		time.Sleep(5 * time.Second)
	}

	assert.NoError(t, err, "HTTP request to application should succeed")
	assert.NotNil(t, resp, "HTTP response should not be nil")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "Application should return 200 OK")
	t.Logf("Successfully pinged application at %s, got 200 OK", url)

	t.Log("VM installation test completed successfully")
}
