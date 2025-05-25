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

	// DockerEnvironment represents tests running in local Docker container
	DockerEnvironment Environment = "docker"
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
	UseDocker      bool     // If true, use Docker instead of Multipass
	DockerImage    string   // Docker image to use
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
		Debug:       os.Getenv("DEBUG") == "1",
		UseDocker:   os.Getenv("USE_DOCKER") == "1" || runtime.GOOS == "darwin",
		DockerImage: "ubuntu:22.04",
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
	} else if config.UseDocker {
		env = DockerEnvironment
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

	switch r.env {
	case CIEnvironment:
		return r.runInCI()
	case DockerEnvironment:
		return r.runInDocker()
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

// runInDocker runs the command in a Docker container
func (r *TestRunner) runInDocker() error {
	r.logf("Running in Docker environment")

	// Check for Docker availability
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker not found, please install it first: %w", err)
	}

	// Create a unique container name
	containerName := fmt.Sprintf("infinity-test-%d", time.Now().Unix())
	r.logf("Creating Docker container: %s", containerName)

	// Create the test container
	createArgs := []string{
		"run", "-d", "--privileged",
		"--name", containerName,
	}

	// Map environment variables
	for k, v := range r.Config.EnvVars {
		createArgs = append(createArgs, "-e", fmt.Sprintf("%s=%s", k, v))
	}

	// Set standard test environment
	createArgs = append(createArgs, "-e", "ENV=test")

	// Add the image
	createArgs = append(createArgs, r.Config.DockerImage)

	// Add a command that keeps container running
	createArgs = append(createArgs, "sleep", "infinity")

	r.logf("Creating container with: docker %s", strings.Join(createArgs, " "))
	createCmd := exec.Command("docker", createArgs...)
	createOutput, err := createCmd.CombinedOutput()
	if err != nil {
		r.logf("Container creation failed: %s", string(createOutput))
		return fmt.Errorf("failed to create container: %w", err)
	}
	containerID := strings.TrimSpace(string(createOutput))
	r.logf("Container created: %s", containerID)

	// Ensure cleanup
	defer func() {
		r.logf("Cleaning up container: %s", containerName)
		exec.Command("docker", "stop", containerName).Run()
		exec.Command("docker", "rm", containerName).Run()
	}()

	// Copy binary to container
	r.logf("Copying binary to container")
	copyCmd := exec.Command("docker", "cp", r.Config.BinaryPath, containerName+":/usr/local/bin/infinity-metrics")
	copyOutput, err := copyCmd.CombinedOutput()
	if err != nil {
		r.logf("Copy failed: %s", string(copyOutput))
		return fmt.Errorf("failed to copy binary: %w", err)
	}

	// Make binary executable
	chmodCmd := exec.Command("docker", "exec", containerName, "chmod", "+x", "/usr/local/bin/infinity-metrics")
	if err := chmodCmd.Run(); err != nil {
		return fmt.Errorf("failed to make binary executable: %w", err)
	}

	// Run the command in the container
	r.logf("Running command in container: infinity-metrics %s", strings.Join(r.Config.Args, " "))
	execArgs := []string{"exec", "-i"}
	execArgs = append(execArgs, containerName, "/usr/local/bin/infinity-metrics")
	execArgs = append(execArgs, r.Config.Args...)

	cmd := exec.Command("docker", execArgs...)
	cmd.Stdin = strings.NewReader(r.Config.StdinInput)
	cmd.Stdout = io.MultiWriter(&r.stdout, r.Logger)
	cmd.Stderr = io.MultiWriter(&r.stderr, r.Logger)

	// Run with timeout
	return r.runWithTimeout(cmd)
}

// runLocally runs the command in a multipass VM
func (r *TestRunner) runLocally() error {
	r.logf("Running in local environment with Multipass VM")

	// Check for multipass availability
	if _, err := exec.LookPath("multipass"); err != nil {
		return fmt.Errorf("multipass not found, please install it first: %w", err)
	}

	// Make sure VM name is not empty
	if r.Config.VMName == "" {
		r.Config.VMName = fmt.Sprintf("infinity-test-%d", time.Now().Unix())
		r.logf("VM name was empty, generated name: %s", r.Config.VMName)
	}

	// Clean up existing VM if needed
	r.logf("Cleaning up any existing VM: %s", r.Config.VMName)
	cleanCmd := exec.Command("multipass", "delete", r.Config.VMName, "--purge")
	cleanCmd.Run() // Ignore errors

	// Check multipass version for feature support
	mpVersion, _ := exec.Command("multipass", "version").Output()
	r.logf("Multipass version: %s", string(mpVersion))

	// Create VM - use available images
	r.logf("Creating new VM: %s", r.Config.VMName)

	// Check if running on Apple Silicon (ARM64)
	onAppleSilicon := runtime.GOARCH == "arm64" && runtime.GOOS == "darwin"

	// Handle architecture for Apple Silicon Macs
	args := []string{"launch"}
	if onAppleSilicon {
		r.logf("Detected Apple Silicon Mac")

		// Add platform compatibility environment variable
		r.Config.EnvVars["PLATFORM_CHECK_DISABLED"] = "1"
		r.logf("VM will run with ARM64 architecture - Docker images must be ARM64 compatible")
	}

	// Add standard arguments
	args = append(args, "22.04", "--name", r.Config.VMName)

	// Add other flags
	args = append(args, r.Config.MultipassFlags...)

	r.logf("Launching VM with command: multipass %s", strings.Join(args, " "))
	launchCmd := exec.Command("multipass", args...)
	launchOutput := &bytes.Buffer{}
	launchCmd.Stdout = io.MultiWriter(launchOutput, r.Logger)
	launchCmd.Stderr = io.MultiWriter(launchOutput, r.Logger)

	if err := launchCmd.Run(); err != nil {
		r.logf("VM launch output: %s", launchOutput.String())
		return fmt.Errorf("failed to launch VM: %w", err)
	}

	// Wait for VM to be ready with better SSH connectivity checks
	r.logf("Waiting for VM to be ready with SSH connectivity")
	r.logf("Running ./scripts/wait-for-vm.sh %s", r.Config.VMName)

	var vmIsReady bool

	// Use wait-for-vm.sh script if it exists
	_, waitScriptErr := os.Stat("./scripts/wait-for-vm.sh")
	if waitScriptErr == nil {
		waitCmd := exec.Command("./scripts/wait-for-vm.sh", r.Config.VMName)
		waitCmd.Stdout = r.Logger
		waitCmd.Stderr = r.Logger
		if err := waitCmd.Run(); err == nil {
			r.logf("VM is ready according to wait-for-vm.sh script")
			vmIsReady = true
		} else {
			r.logf("Wait script failed, falling back to built-in wait: %v", err)
		}
	}

	// Fallback to built-in wait mechanism if not ready yet
	if !vmIsReady {
		for i := 0; i < 60; i++ { // Give up to 60 seconds
			cmd := exec.Command("multipass", "info", r.Config.VMName)
			output, err := cmd.CombinedOutput()
			outputStr := string(output)
			r.logf("VM status check attempt %d/60: %v", i+1, err)

			if err == nil {
				r.logf("VM info output: %s", outputStr)
				if strings.Contains(outputStr, "State:") && strings.Contains(outputStr, "Running") {
					// Check SSH connectivity specifically
					sshCmd := exec.Command("multipass", "exec", r.Config.VMName, "--", "echo", "SSH connection test")
					sshOutput, sshErr := sshCmd.CombinedOutput()
					if sshErr == nil && strings.Contains(string(sshOutput), "SSH connection test") {
						vmIsReady = true
						r.logf("VM is ready with SSH connectivity after %d seconds", i+1)
						break
					} else {
						r.logf("VM is running but SSH not ready yet. Waiting... SSH err: %v", sshErr)
					}
				}
			}
			time.Sleep(3 * time.Second) // Longer wait between checks
		}
	}

	if !vmIsReady {
		// Run the diagnostics script if available
		_, diagScriptErr := os.Stat("./scripts/verify-vm.sh")
		if diagScriptErr == nil {
			r.logf("Running VM diagnostic script...")
			diagCmd := exec.Command("./scripts/verify-vm.sh", r.Config.VMName)
			diagCmd.Stdout = r.Logger
			diagCmd.Stderr = r.Logger
			diagCmd.Run() // Ignore errors
		}

		// Get detailed info about the VM for debugging
		infoCmd := exec.Command("multipass", "info", r.Config.VMName, "--format", "yaml")
		infoOutput, _ := infoCmd.CombinedOutput()
		r.logf("Detailed VM info: \n%s", string(infoOutput))

		// Try one more time with a different string check to be sure
		finalCheckCmd := exec.Command("multipass", "list")
		finalOutput, _ := finalCheckCmd.CombinedOutput()
		r.logf("Multipass list output: \n%s", string(finalOutput))

		// Check VM architecture
		archCmd := exec.Command("multipass", "exec", r.Config.VMName, "--", "uname", "-m")
		archOutput, archErr := archCmd.CombinedOutput()
		if archErr == nil {
			r.logf("VM architecture: %s", string(archOutput))
		} else {
			r.logf("Failed to determine VM architecture: %v", archErr)
		}

		// Try restarting the VM as a last resort
		r.logf("Trying to restart VM as a last resort...")
		restartCmd := exec.Command("multipass", "restart", r.Config.VMName)
		if restartErr := restartCmd.Run(); restartErr != nil {
			r.logf("Failed to restart VM: %v", restartErr)
		}

		// Wait a moment and try one final check
		time.Sleep(5 * time.Second)
		if sshFinalCmd := exec.Command("multipass", "exec", r.Config.VMName, "--", "echo", "Final SSH test"); sshFinalCmd.Run() == nil {
			r.logf("Final SSH test succeeded after VM restart!")
			vmIsReady = true
		}
	}

	if !vmIsReady {
		return fmt.Errorf("timeout waiting for VM to be ready with SSH connectivity")
	}

	// Verify the architecture
	archCmd := exec.Command("multipass", "exec", r.Config.VMName, "--", "uname", "-m")
	archOutput, archErr := archCmd.CombinedOutput()
	if archErr == nil {
		arch := strings.TrimSpace(string(archOutput))
		r.logf("VM architecture: %s", arch)
		if arch != "x86_64" {
			r.logf("WARNING: VM is not running with x86_64 architecture!")
		}
	}

	// Before running the installer command, set the ENV variable in the VM
	envSetCmd := exec.Command("multipass", "exec", r.Config.VMName, "--", "sudo", "sh", "-c", "echo 'export ENV=test' >> /etc/environment")
	envSetCmd.Run() // Run this before your installer command

	// Copy binary to VM
	r.logf("Copying binary to VM")
	copyCmd := exec.Command("multipass", "transfer", r.Config.BinaryPath, fmt.Sprintf("%s:/home/ubuntu/infinity-metrics", r.Config.VMName))
	copyCmd.Stdout = r.Logger
	copyCmd.Stderr = r.Logger

	if err := copyCmd.Run(); err != nil {
		return fmt.Errorf("failed to copy binary to VM: %w", err)
	}

	// Make binary executable and move to system location
	r.logf("Making binary executable")
	execCmd := exec.Command("multipass", "exec", r.Config.VMName, "--", "chmod", "+x", "/home/ubuntu/infinity-metrics")
	execCmd.Stdout = r.Logger
	execCmd.Stderr = r.Logger

	if err := execCmd.Run(); err != nil {
		return fmt.Errorf("failed to make binary executable: %w", err)
	}

	r.logf("Moving binary to system location")
	moveCmd := exec.Command("multipass", "exec", r.Config.VMName, "--", "sudo", "mv", "/home/ubuntu/infinity-metrics", "/usr/local/bin/infinity-metrics")
	moveCmd.Stdout = r.Logger
	moveCmd.Stderr = r.Logger

	if err := moveCmd.Run(); err != nil {
		return fmt.Errorf("failed to move binary: %w", err)
	}

	// Run the command in VM
	r.logf("Running command in VM: infinity-metrics %s", strings.Join(r.Config.Args, " "))

	// Set environment variables in the VM before running the command
	for k, v := range r.Config.EnvVars {
		r.logf("Setting environment variable in VM: %s=%s", k, v)
		envCmd := exec.Command("multipass", "exec", r.Config.VMName, "--", "sudo", "sh", "-c",
			fmt.Sprintf("echo 'export %s=%s' >> /etc/environment", k, v))
		envOutput, err := envCmd.CombinedOutput()
		if err != nil {
			r.logf("Failed to set environment variable %s: %v\nOutput: %s", k, err, string(envOutput))
		}
	}

	// Create the command to run with stdin input
	cmdParts := []string{"exec", r.Config.VMName, "--", "sudo", "/usr/local/bin/infinity-metrics"}
	cmdParts = append(cmdParts, r.Config.Args...)

	cmd := exec.Command("multipass", cmdParts...)
	cmd.Stdin = strings.NewReader(r.Config.StdinInput)
	cmd.Stdout = io.MultiWriter(&r.stdout, r.Logger)
	cmd.Stderr = io.MultiWriter(&r.stderr, r.Logger)

	// Set environment variables
	cmd.Env = os.Environ()
	for k, v := range r.Config.EnvVars {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Run with timeout
	err := r.runWithTimeout(cmd)

	// If configured to keep VM, don't delete it
	if os.Getenv("KEEP_VM") != "1" {
		r.logf("Cleaning up VM: %s", r.Config.VMName)
		cleanupCmd := exec.Command("multipass", "delete", r.Config.VMName, "--purge")
		cleanupCmd.Run() // Ignore errors
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
