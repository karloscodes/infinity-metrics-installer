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

	// Get installer binary path
	installerPath := os.Getenv("INSTALLER_BINARY")
	if installerPath == "" {
		installerPath = filepath.Join(projectRoot, "bin", "infinity-metrics-installer")
		t.Logf("INSTALLER_BINARY not set, using default path: %s", installerPath)
	} else {
		t.Logf("INSTALLER_BINARY set to: %s", installerPath)
	}

	// Get updater binary path
	updaterPath := os.Getenv("UPDATER_BINARY")
	if updaterPath == "" {
		updaterPath = filepath.Join(projectRoot, "bin", "infinity-metrics-updater")
		t.Logf("UPDATER_BINARY not set, using default path: %s", updaterPath)
	} else {
		t.Logf("UPDATER_BINARY set to: %s", updaterPath)
	}

	// Check if the binaries exist
	assert.FileExists(t, installerPath, "Installer binary should exist")
	assert.FileExists(t, updaterPath, "Updater binary should exist")

	// Path to the run_in_vm.sh script
	vmScriptPath := filepath.Join(projectRoot, "tests", "run_in_vm.sh")
	assert.FileExists(t, vmScriptPath, "VM script should exist")

	// Run the script with both binary paths
	cmd := exec.Command(vmScriptPath, "--binary="+installerPath, "--updater="+updaterPath)

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	t.Log("Starting VM installation test...")
	err = cmd.Run()

	// Log the captured output
	outputStr := stdout.String()
	errorStr := stderr.String()
	if outputStr != "" {
		t.Logf("Installer Output:\n%s", outputStr)
	}
	if errorStr != "" {
		t.Logf("Installer Errors:\n%s", errorStr)
	}

	// Assert script ran successfully
	assert.NoError(t, err, "VM installation script should run successfully")

	// Assert installer completed without errors
	// assert.NotContains(t, outputStr, "[ERROR] Installation failed", "Installer should not report a failure")
	// assert.Contains(t, outputStr, "[SUCCESS] Docker installed successfully", "Docker should be installed")
	// assert.Contains(t, outputStr, "[SUCCESS] Docker Swarm initialized successfully", "Swarm should be initialized")
	// assert.Contains(t, outputStr, "[SUCCESS] Repository cloned successfully", "Repository should be cloned")
	// assert.Contains(t, outputStr, "[SUCCESS] Configuration saved successfully", "Configuration should be saved")

	// Extract VM IP from multipass info infinity-test-vm
	// Output example:
	//
	// Name:           infinity-test-vm
	// State:          Running
	// Snapshots:      0
	// IPv4:           192.168.64.27
	// Release:        Ubuntu 22.04.5 LTS
	// Image hash:     46113bedf45e (Ubuntu 22.04 LTS)
	// CPU(s):         2
	// Load:           0.52 0.11 0.04
	// Disk usage:     1.6GiB out of 9.6GiB
	// Memory usage:   183.3MiB out of 1.9GiB
	// Mounts:         --
	// Extract VM IP from multipass info infinity-test-vm
	cmd = exec.Command("bash", "-c", "multipass info infinity-test-vm | grep IPv4 | awk '{print $2}'")
	var ipOutput bytes.Buffer
	cmd.Stdout = &ipOutput
	err = cmd.Run()
	assert.NoError(t, err, "Failed to get VM IP")

	vmIP := strings.TrimSpace(ipOutput.String())
	assert.NotEmpty(t, vmIP, "VM IP should be found")
	t.Logf("VM IP extracted: %s", vmIP)

	// Ping the application via HTTP
	url := "http://" + vmIP // Adjust port/path if needed (e.g., ":8080/health")
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
