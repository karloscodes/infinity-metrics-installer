package testrunner

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
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
	vmProvider VMProvider
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
		vmProvider: NewVMProvider(),
	}
}

// Run executes the command
func (r *TestRunner) Run() error {
	r.logf("Starting test in %s environment", r.env)
	r.logf("Binary path: %s", r.Config.BinaryPath)
	r.logf("Environment variables: %v", r.Config.EnvVars)

	switch r.env {
	case CIEnvironment:
		return r.runInCI()
	default:
		return r.runLocally()
	}
}

// runInCI runs the command directly in CI
func (r *TestRunner) runInCI() error {
	r.logf("Running directly in CI environment")

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

// runLocally runs the command in a VM
func (r *TestRunner) runLocally() error {
	r.logf("Running in local environment with %s VM", r.vmProvider.Name())

	// Check for VM provider
	if !r.vmProvider.IsInstalled() {
		return fmt.Errorf("%s not found, please install it first", r.vmProvider.Name())
	}

	// Make sure VM name is not empty
	if r.Config.VMName == "" {
		r.Config.VMName = fmt.Sprintf("infinity-test-%d", time.Now().Unix())
		r.logf("VM name was empty, generated name: %s", r.Config.VMName)
	}

	// Clean up existing VM if needed
	r.logf("Cleaning up any existing VM with the same name: %s", r.Config.VMName)
	r.vmProvider.Delete(r.Config.VMName) // Ignore errors

	// Create VM
	r.logf("Creating new VM: %s", r.Config.VMName)

	// Prepare VM creation arguments
	args := r.Config.MultipassFlags

	// Add standard arguments for Ubuntu 22.04
	args = append(args, "22.04")

	r.logf("Launching VM with %s", r.vmProvider.Name())
	if err := r.vmProvider.Create(r.Config.VMName, args); err != nil {
		r.logf("VM launch failed: %s", err)
		return fmt.Errorf("failed to launch VM: %w", err)
	}

	// Wait for VM to be ready with better SSH connectivity checks
	r.logf("Waiting for VM to be ready with SSH connectivity")
	r.logf("Waiting for VM to be ready...")
	for i := 0; i < 30; i++ {
		_, err := r.vmProvider.Exec(r.Config.VMName, "echo", "VM is ready")
		if err == nil {
			break
		}
		if i == 29 {
			return fmt.Errorf("VM did not become ready in time")
		}
		time.Sleep(2 * time.Second)
	}

	// Check VM architecture
	r.logf("Checking VM architecture")
	archOutput, archErr := r.vmProvider.Exec(r.Config.VMName, "uname", "-m")
	if archErr == nil {
		arch := strings.TrimSpace(archOutput)
		r.logf("VM architecture: %s", arch)
		if arch != "x86_64" {
			r.logf("WARNING: VM is not running with x86_64 architecture!")
			if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
				r.logf("On Apple Silicon Macs, consider using Parallels Desktop for x86_64 VM support")
			}
		}
	}

	// Before running the installer command, set the ENV variable in the VM
	r.vmProvider.Exec(r.Config.VMName, "sudo", "sh", "-c", "echo 'export ENV=test' >> /etc/environment")

	// Copy binary to VM
	r.logf("Copying binary to VM")
	if err := r.vmProvider.Transfer(r.Config.BinaryPath, fmt.Sprintf("%s:/home/ubuntu/infinity-metrics", r.Config.VMName)); err != nil {
		return fmt.Errorf("failed to copy binary to VM: %w", err)
	}

	// Make binary executable and move to system location
	r.logf("Making binary executable")
	if _, err := r.vmProvider.Exec(r.Config.VMName, "chmod", "+x", "/home/ubuntu/infinity-metrics"); err != nil {
		return fmt.Errorf("failed to make binary executable: %w", err)
	}

	r.logf("Moving binary to system location")
	if _, err := r.vmProvider.Exec(r.Config.VMName, "sudo", "mv", "/home/ubuntu/infinity-metrics", "/usr/local/bin/infinity-metrics"); err != nil {
		return fmt.Errorf("failed to move binary: %w", err)
	}

	// Run the command in VM
	r.logf("Running command in VM: infinity-metrics %s", strings.Join(r.Config.Args, " "))

	// Set environment variables in the VM before running the command
	for k, v := range r.Config.EnvVars {
		r.logf("Setting environment variable in VM: %s=%s", k, v)
		output, err := r.vmProvider.Exec(r.Config.VMName, "sudo", "sh", "-c",
			fmt.Sprintf("echo 'export %s=%s' >> /etc/environment", k, v))
		if err != nil {
			r.logf("Failed to set environment variable %s: %v\nOutput: %s", k, err, output)
		}
	}

	// Create a command string for the VM
	cmdStr := "/usr/local/bin/infinity-metrics " + strings.Join(r.Config.Args, " ")
	
	// For VM providers that don't support stdin directly, we need a different approach
	// We'll write the input to a file and use it in the VM
	inputFile := "/tmp/infinity-metrics-input.txt"
	if _, err := r.vmProvider.Exec(r.Config.VMName, "sudo", "bash", "-c", fmt.Sprintf("cat > %s", inputFile), r.Config.StdinInput); err != nil {
		return fmt.Errorf("failed to create input file in VM: %w", err)
	}
	
	// Run the command with input from the file
	cmdWithInput := fmt.Sprintf("sudo bash -c 'cat %s | sudo %s'", inputFile, cmdStr)
	
	// Execute the command in the VM
	var stdout, stderr bytes.Buffer
	output, err := r.vmProvider.Exec(r.Config.VMName, "bash", "-c", cmdWithInput)
	
	// Capture output
	stdout.WriteString(output)
	if err != nil {
		stderr.WriteString(fmt.Sprintf("Command failed: %v", err))
	}
	
	// Copy output to the runner's buffers
	r.stdout.Write(stdout.Bytes())
	r.stderr.Write(stderr.Bytes())
	
	// Clean up the input file
	r.vmProvider.Exec(r.Config.VMName, "sudo", "rm", "-f", inputFile)

	// If configured to keep VM, don't delete it
	if os.Getenv("KEEP_VM") != "1" {
		r.logf("Cleaning up VM: %s", r.Config.VMName)
		r.vmProvider.Delete(r.Config.VMName) // Ignore errors
	} else {
		r.logf("Keeping VM for inspection: %s", r.Config.VMName)
	}

	return err
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
