package testrunner

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Environment represents where tests are running
type Environment string

const (
	// LocalEnvironment represents tests running locally
	LocalEnvironment Environment = "local"

	// CIEnvironment represents tests running in GitHub Actions
	CIEnvironment Environment = "ci"
)

// Config holds configuration for the test runner
type Config struct {
	BinaryPath     string
	Timeout        time.Duration
	StdinInput     string
	EnvVars        map[string]string
	Args           []string
	WorkingDir     string
	Debug          bool
	VMName         string   // Only used for local environment
	MultipassFlags []string // Only used for local environment
	DirectRun      bool     // If true, run directly without VM even in local environment
}

// DefaultConfig returns a Config with default values
func DefaultConfig() Config {
	return Config{
		Timeout: 10 * time.Minute, // Increased timeout to 10 minutes
		EnvVars: make(map[string]string),
		VMName:  "infinity-test-vm",
		MultipassFlags: []string{
			"--memory", "2G",
			"--disk", "10G",
			"--cpus", "2",
		},
		Debug: os.Getenv("DEBUG") == "1",
	}
}

// TestRunner handles running commands in different environments
type TestRunner struct {
	Config Config
	env    Environment
	stdout bytes.Buffer
	stderr bytes.Buffer
	Logger io.Writer
}

// NewTestRunner creates a new TestRunner
func NewTestRunner(config Config) *TestRunner {
	env := LocalEnvironment
	if os.Getenv("GITHUB_ACTIONS") == "true" || os.Getenv("GITHUB_RUN_NUMBER") != "" {
		env = CIEnvironment
	}

	return &TestRunner{
		Config: config,
		env:    env,
		Logger: os.Stdout,
	}
}

// Run executes the command in the appropriate environment.
// - In CI or if DirectRun is true, runs the binary directly on the host (see runDirectly).
// - Otherwise, runs the binary inside a fresh Multipass VM (see runInVM).
func (r *TestRunner) Run() error {
	r.logf("Starting test in %s environment", r.env)
	r.logf("Binary path: %s", r.Config.BinaryPath)
	r.logf("Environment variables: %v", r.Config.EnvVars)

	if r.env == CIEnvironment || r.Config.DirectRun {
		return r.runDirectly()
	} else {
		return r.runInVM()
	}
}

// runDirectly runs the installer binary directly on the host system.
// Used for fast feedback in CI or local dev, but does not test real system effects.
func (r *TestRunner) runDirectly() error {
	if r.Config.DirectRun {
		r.logf("Running directly (DirectRun mode)")
	} else {
		r.logf("Running directly in CI environment")
	}

	// Create the command
	cmd := exec.Command(r.Config.BinaryPath, r.Config.Args...)

	// Setup input/output
	cmd.Stdin = strings.NewReader(r.Config.StdinInput)
	cmd.Stdout = io.MultiWriter(&r.stdout, r.Logger)
	cmd.Stderr = io.MultiWriter(&r.stderr, r.Logger)

	// Set environment variables
	cmd.Env = os.Environ()
	for k, v := range r.Config.EnvVars {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	if r.Config.WorkingDir != "" {
		cmd.Dir = r.Config.WorkingDir
	}

	r.logf("Executing command: %s %s", cmd.Path, strings.Join(cmd.Args[1:], " "))

	// Run with timeout
	return r.runWithTimeout(cmd)
}

// runInVM provisions a fresh Multipass VM, injects your SSH key, copies the binary and script, and runs the installer inside the VM.
// This simulates a real user/server environment and tests the full install process, including system-level effects.
func (r *TestRunner) runInVM() error {
	r.logf("Running in local environment with Multipass VM")

	// Check for multipass availability
	if _, err := exec.LookPath("multipass"); err != nil {
		return fmt.Errorf("multipass not found, please install it first: %w", err)
	}

	// Clean up existing VM if needed
	r.logf("Cleaning up any existing VM: %s", r.Config.VMName)
	cleanCmd := exec.Command("multipass", "delete", r.Config.VMName, "--purge")
	cleanCmd.Run() // Ignore errors

	// Prepare cloud-init with SSH key for VM access
	sshKeyPath := os.Getenv("SSH_KEY_PATH")
	if sshKeyPath == "" {
		sshKeyPath = filepath.Join(os.Getenv("HOME"), ".ssh", "id_ed25519.pub")
	}
	// If the SSH public key does not exist, generate a new Ed25519 key pair
	if _, err := os.Stat(sshKeyPath); os.IsNotExist(err) {
		privateKeyPath := strings.TrimSuffix(sshKeyPath, ".pub")
		r.logf("SSH public key not found at %s, generating new Ed25519 key pair...", sshKeyPath)
		cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-f", privateKeyPath, "-N", "")
		cmd.Stdout = r.Logger
		cmd.Stderr = r.Logger
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to generate SSH key pair: %w", err)
		}
	}
	pubKeyBytes, err := os.ReadFile(sshKeyPath)
	if err != nil {
		return fmt.Errorf("failed to read SSH public key (%s): %w", sshKeyPath, err)
	}
	cloudInit := fmt.Sprintf(`#cloud-config\nusers:\n  - default\n  - name: ubuntu\n    ssh_authorized_keys:\n      - %s\n`, strings.TrimSpace(string(pubKeyBytes)))
	cloudInitPath := filepath.Join(os.TempDir(), "cloud-init.yaml")
	if err := os.WriteFile(cloudInitPath, []byte(cloudInit), 0o644); err != nil {
		return fmt.Errorf("failed to write cloud-init file: %w", err)
	}

	// Create new VM with cloud-init and any extra flags
	r.logf("Creating new VM: %s with cloud-init", r.Config.VMName)
	args := []string{"launch", "22.04", "--name", r.Config.VMName, "--cpus", "2", "--memory", "2G", "--disk", "10G", "--cloud-init", cloudInitPath}
	args = append(args, r.Config.MultipassFlags...)

	launchCmd := exec.Command("multipass", args...)
	launchOutput := &bytes.Buffer{}
	launchCmd.Stdout = io.MultiWriter(launchOutput, r.Logger)
	launchCmd.Stderr = io.MultiWriter(launchOutput, r.Logger)

	if err := launchCmd.Run(); err != nil {
		r.logf("VM launch output: %s", launchOutput.String())
		return fmt.Errorf("failed to launch VM: %w", err)
	}

	// Wait for the VM to be ready for SSH/SCP
	r.logf("Waiting for VM to fully initialize...")
	time.Sleep(30 * time.Second)

	// Use a file-based approach to check if VM is ready for SSH/SCP
	r.logf("Testing VM connectivity using file operations (SCP)...")

	// Create a temp directory for testing
	tempDir, err := os.MkdirTemp("", "multipass-test-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a test file
	testFile := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0o644); err != nil {
		return fmt.Errorf("failed to create test file: %w", err)
	}

	// Try to copy file to VM using scp/ssh instead of multipass transfer for readiness check
	vmReady := false
	for i := 0; i < 30; i++ {
		r.logf("VM readiness check %d/30", i+1)

		err := r.CopyFileToVMOverSSH(testFile, "/tmp/test.txt")
		if err == nil {
			r.logf("VM is ready for file operations after %d attempts", i+1)
			vmReady = true
			break
		} else {
			r.logf("SCP test failed (attempt %d): %v", i+1, err)
			time.Sleep(2 * time.Second)
		}
	}

	if !vmReady {
		return fmt.Errorf("VM failed to become ready for file operations")
	}

	// Copy binary to VM using scp/ssh instead of multipass transfer
	r.logf("Copying binary to VM using scp/ssh")
	if err := r.CopyFileToVMOverSSH(r.Config.BinaryPath, "/home/ubuntu/infinity-metrics"); err != nil {
		return fmt.Errorf("failed to copy binary to VM via scp: %w", err)
	}

	// Create a script file locally that will set up and run the installer
	scriptContent := fmt.Sprintf(`#!/bin/bash
set -e

# Make binary executable and move to system location
chmod +x /home/ubuntu/infinity-metrics
sudo mv /home/ubuntu/infinity-metrics /usr/local/bin/infinity-metrics

# Set environment variables
export ENV=test
%s

# Run the installer with provided arguments and input
echo '%s' | sudo /usr/local/bin/infinity-metrics %s

# Check the result
RESULT=$?
if [ $RESULT -ne 0 ]; then
  echo "Installation failed with exit code: $RESULT"
  exit $RESULT
fi

echo "Installation completed successfully"
`,
		r.buildEnvExports(),
		strings.ReplaceAll(r.Config.StdinInput, "'", "''"),
		strings.Join(r.Config.Args, " "))

	// Write script to temp file
	scriptPath := filepath.Join(tempDir, "run_installer.sh")
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755); err != nil {
		return fmt.Errorf("failed to create script: %w", err)
	}

	r.logf("Created installer script:\n%s", scriptContent)

	// Copy script to VM using scp/ssh instead of multipass transfer
	scriptVMPath := "/home/ubuntu/run_installer.sh"
	if err := r.CopyFileToVMOverSSH(scriptPath, scriptVMPath); err != nil {
		return fmt.Errorf("failed to copy script to VM via scp: %w", err)
	}

	// Execute the script using exec
	r.logf("Executing installer script in VM")
	execCmd := exec.Command("multipass", "exec", r.Config.VMName, "--", "bash", "/home/ubuntu/run_installer.sh")
	execCmd.Stdout = io.MultiWriter(&r.stdout, r.Logger)
	execCmd.Stderr = io.MultiWriter(&r.stderr, r.Logger)

	// Run with timeout
	err = r.runWithTimeout(execCmd)

	// If configured to keep VM, don't delete it
	if os.Getenv("KEEP_VM") != "1" && err == nil {
		r.logf("Cleaning up VM: %s", r.Config.VMName)
		cleanupCmd := exec.Command("multipass", "delete", r.Config.VMName, "--purge")
		cleanupCmd.Run() // Ignore errors
	} else {
		r.logf("Keeping VM for inspection: %s", r.Config.VMName)
	}

	return err
}

// GetVMIP returns the IPv4 address of the Multipass VM
func (r *TestRunner) GetVMIP() (string, error) {
	cmd := exec.Command("multipass", "info", r.Config.VMName)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if strings.Contains(line, "IPv4") {
			parts := strings.Fields(line)
			if len(parts) > 1 {
				return parts[1], nil
			}
		}
	}
	return "", fmt.Errorf("IP not found for VM %s", r.Config.VMName)
}

// RunSSHCommand runs a shell command in the VM via SSH (requires SSH enabled in the VM)
func (r *TestRunner) RunSSHCommand(command string) (string, error) {
	ip, err := r.GetVMIP()
	if err != nil {
		return "", err
	}
	sshCmd := exec.Command("ssh", "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null", "ubuntu@"+ip, command)
	out, err := sshCmd.CombinedOutput()
	return string(out), err
}

// CopyFileToVM copies a file to the VM using multipass transfer
func (r *TestRunner) CopyFileToVM(localPath, remotePath string) error {
	cmd := exec.Command("multipass", "transfer", localPath, fmt.Sprintf("%s:%s", r.Config.VMName, remotePath))
	return cmd.Run()
}

// CopyFileToVMOverSSH copies a file to the VM using scp/ssh
func (r *TestRunner) CopyFileToVMOverSSH(localPath, remotePath string) error {
	ip, err := r.GetVMIP()
	if err != nil {
		return err
	}
	scpCmd := exec.Command("scp", "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null", localPath, fmt.Sprintf("ubuntu@%s:%s", ip, remotePath))
	out, err := scpCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("scp failed: %v, output: %s", err, string(out))
	}
	return nil
}

// CheckServiceAvailability checks if the service is available at the given URL.
// In VM mode, it uses SSH to curl from inside the VM. In direct mode, it curls locally.
func (r *TestRunner) CheckServiceAvailability(url string, attempts int, t interface{ Logf(string, ...interface{}); Errorf(string, ...interface{}) }) (success bool, is302 bool, finalOutput string) {
	for i := 0; i < attempts; i++ {
		var output string
		var err error
		if r.env == LocalEnvironment && !r.Config.DirectRun {
			// VM mode: curl via SSH
			sshCmd := fmt.Sprintf("curl -k -s -o /dev/null -w '%%{http_code}' %s", url)
			output, err = r.RunSSHCommand(sshCmd)
		} else {
			// Direct mode: curl locally
			cmd := exec.Command("curl", "-k", "-s", "-o", "/dev/null", "-w", "%{http_code}", url)
			out, e := cmd.CombinedOutput()
			output = strings.TrimSpace(string(out))
			err = e
		}
		outputStr := strings.TrimSpace(output)
		finalOutput = outputStr
		t.Logf("Service check attempt %d/%d, result: %s, error: %v", i+1, attempts, outputStr, err)
		if err == nil && outputStr == "302" {
			success = true
			is302 = true
			break
		} else if err == nil && outputStr == "200" {
			success = true
		}
		time.Sleep(5 * time.Second)
	}
	return
}

// buildEnvExports creates export statements for environment variables
func (r *TestRunner) buildEnvExports() string {
	var exports []string
	for k, v := range r.Config.EnvVars {
		exports = append(exports, fmt.Sprintf("export %s='%s'", k, v))
	}
	return strings.Join(exports, "\n")
}

// runWithTimeout runs a command with the configured timeout
func (r *TestRunner) runWithTimeout(cmd *exec.Cmd) error {
	err := cmd.Start()
	if err != nil {
		return fmt.Errorf("failed to start command: %w", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(r.Config.Timeout):
		cmd.Process.Kill()
		return fmt.Errorf("command timed out after %v", r.Config.Timeout)
	}
}

// Stdout returns the stdout output
func (r *TestRunner) Stdout() string {
	return r.stdout.String()
}

// Stderr returns the stderr output
func (r *TestRunner) Stderr() string {
	return r.stderr.String()
}

// logf logs a message to the logger if debug is enabled
func (r *TestRunner) logf(format string, args ...interface{}) {
	if r.Config.Debug {
		fmt.Fprintf(r.Logger, "[TestRunner] "+format+"\n", args...)
	}
}
