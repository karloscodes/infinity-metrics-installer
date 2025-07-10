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

// Run executes the command
func (r *TestRunner) Run() error {
	r.logf("Starting test in %s environment", r.env)
	r.logf("Binary path: %s", r.Config.BinaryPath)
	r.logf("Environment variables: %v", r.Config.EnvVars)

	if r.env == CIEnvironment || r.Config.DirectRun {
		return r.runDirectly()
	} else {
		return r.runLocally()
	}
}

// runDirectly runs the command directly (used in CI or when DirectRun is true)
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

// runLocally runs the command in a multipass VM with improved error handling
func (r *TestRunner) runLocally() error {
	r.logf("Running in local environment with Multipass VM")

	// Check for multipass availability
	if _, err := exec.LookPath("multipass"); err != nil {
		return fmt.Errorf("multipass not found, please install it first: %w", err)
	}

	// Clean up existing VM if needed
	r.logf("Cleaning up any existing VM: %s", r.Config.VMName)
	cleanCmd := exec.Command("multipass", "delete", r.Config.VMName, "--purge")
	cleanCmd.Run() // Ignore errors

	// Create new VM with more explicit configuration
	r.logf("Creating new VM: %s", r.Config.VMName)
	
	args := []string{"launch", "22.04", "--name", r.Config.VMName, "--cpus", "2", "--memory", "2G", "--disk", "10G"}

	// Add any additional flags
	args = append(args, r.Config.MultipassFlags...)

	launchCmd := exec.Command("multipass", args...)
	launchOutput := &bytes.Buffer{}
	launchCmd.Stdout = io.MultiWriter(launchOutput, r.Logger)
	launchCmd.Stderr = io.MultiWriter(launchOutput, r.Logger)

	if err := launchCmd.Run(); err != nil {
		r.logf("VM launch output: %s", launchOutput.String())
		return fmt.Errorf("failed to launch VM: %w", err)
	}

	// Give the VM more time to settle and initialize completely
	r.logf("Waiting for VM to fully initialize...")
	time.Sleep(30 * time.Second)

	// Use a file-based approach to check if VM is ready instead of SSH
	r.logf("Testing VM connectivity using file operations...")
	
	// Create a temp directory for testing
	tempDir, err := os.MkdirTemp("", "multipass-test-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)
	
	// Create a test file
	testFile := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		return fmt.Errorf("failed to create test file: %w", err)
	}
	
	// Try to copy file to VM using transfer (this works better than exec)
	vmReady := false
	for i := 0; i < 30; i++ {
		r.logf("VM readiness check %d/30", i+1)
		
		copyCmd := exec.Command("multipass", "transfer", testFile, fmt.Sprintf("%s:/tmp/test.txt", r.Config.VMName))
		if err := copyCmd.Run(); err == nil {
			r.logf("VM is ready for file operations after %d attempts", i+1)
			vmReady = true
			break
		} else {
			r.logf("Transfer test failed (attempt %d): %v", i+1, err)
			time.Sleep(2 * time.Second)
		}
	}
	
	if !vmReady {
		return fmt.Errorf("VM failed to become ready for file operations")
	}

	// Copy binary to VM using transfer
	r.logf("Copying binary to VM using transfer")
	copyCmd := exec.Command("multipass", "transfer", r.Config.BinaryPath, fmt.Sprintf("%s:/home/ubuntu/infinity-metrics", r.Config.VMName))
	copyCmd.Stdout = r.Logger
	copyCmd.Stderr = r.Logger

	if err := copyCmd.Run(); err != nil {
		return fmt.Errorf("failed to copy binary to VM: %w", err)
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
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
		return fmt.Errorf("failed to create script: %w", err)
	}

	r.logf("Created installer script:\n%s", scriptContent)

	// Copy script to VM
	scriptVMPath := fmt.Sprintf("%s:/home/ubuntu/run_installer.sh", r.Config.VMName)
	copyScriptCmd := exec.Command("multipass", "transfer", scriptPath, scriptVMPath)
	if err := copyScriptCmd.Run(); err != nil {
		return fmt.Errorf("failed to copy script to VM: %w", err)
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
