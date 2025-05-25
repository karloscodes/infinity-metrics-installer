package testrunner

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
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

	if r.env == CIEnvironment {
		return r.runInCI()
	} else {
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

// runLocally runs the command in a multipass VM
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

	// Create new VM
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

	// Wait for VM to be ready
	r.logf("Waiting for VM to be ready")
	vmReady := false
	for i := 0; i < 60; i++ { // Give up to 60 seconds
		cmd := exec.Command("multipass", "info", r.Config.VMName)
		output, err := cmd.CombinedOutput()
		outputStr := string(output)
		r.logf("VM status check attempt %d/60: %v", i+1, err)

		if err == nil {
			r.logf("VM info output: %s", outputStr)
			if strings.Contains(outputStr, "State:") && strings.Contains(outputStr, "Running") {
				vmReady = true
				r.logf("VM is ready after %d seconds", i+1)
				break
			}
		}
		time.Sleep(1 * time.Second)
	}

	if !vmReady {
		// Get detailed info about the VM for debugging
		infoCmd := exec.Command("multipass", "info", r.Config.VMName, "--format", "yaml")
		infoOutput, _ := infoCmd.CombinedOutput()
		r.logf("Detailed VM info: \n%s", string(infoOutput))

		// Try one more time with a different string check to be sure
		finalCheckCmd := exec.Command("multipass", "list")
		finalOutput, _ := finalCheckCmd.CombinedOutput()
		if strings.Contains(string(finalOutput), r.Config.VMName) &&
			strings.Contains(string(finalOutput), "Running") {
			r.logf("VM appears to be ready based on 'multipass list' output")
			vmReady = true
		} else {
			return fmt.Errorf("timeout waiting for VM to be ready")
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
		
		// For environment variables that need to be available to the command
		// we need to set them in both /etc/environment and export them directly
		
		// Add to /etc/environment for persistence
		envCmd := exec.Command("multipass", "exec", r.Config.VMName, "--", "sudo", "sh", "-c",
			fmt.Sprintf("echo 'export %s=%s' >> /etc/environment", k, v))
		envOutput, err := envCmd.CombinedOutput()
		if err != nil {
			r.logf("Failed to set environment variable in /etc/environment %s: %v\nOutput: %s", k, err, string(envOutput))
		}
		
		// Also export it directly for immediate use
		exportCmd := exec.Command("multipass", "exec", r.Config.VMName, "--", "sudo", "sh", "-c",
			fmt.Sprintf("export %s=%s", k, v))
		exportOutput, err := exportCmd.CombinedOutput()
		if err != nil {
			r.logf("Failed to export environment variable %s: %v\nOutput: %s", k, err, string(exportOutput))
		}
	}

	// Create a command that passes environment variables to the sudo command
	// We need to construct a command that preserves environment variables through sudo
	
	// Create a test script that will handle the installation with the input
	scriptContent := "#!/bin/bash\n\n"
	
	// Add environment variables to the script
	for k, v := range r.Config.EnvVars {
		scriptContent += fmt.Sprintf("export %s='%s'\n", k, v)
	}
	
	// Create a simpler script that directly passes environment variables
	scriptContent += fmt.Sprintf(`
# Run the installer with environment variables
echo '%s' | sudo ADMIN_PASSWORD='securepassword123' SKIP_DNS_VALIDATION=1 NONINTERACTIVE=1 /usr/local/bin/infinity-metrics %s

# Check the result
RESULT=$?
if [ $RESULT -ne 0 ]; then
  echo "Installation failed with exit code: $RESULT"
  # Try to get logs
  if [ -f /var/log/infinity-metrics-installer.log ]; then
    echo "Installer log:"
    cat /var/log/infinity-metrics-installer.log
  fi
  exit $RESULT
fi

echo "Installation completed successfully"
`, strings.ReplaceAll(r.Config.StdinInput, "'", "''" ), strings.Join(r.Config.Args, " "))
	
	// Create the script in the VM
	scriptPath := "/tmp/run_infinity_metrics.sh"
	scriptCmd := exec.Command("multipass", "exec", r.Config.VMName, "--", "bash", "-c", fmt.Sprintf("cat > %s << 'EOF'\n%sEOF\nchmod +x %s", scriptPath, scriptContent, scriptPath))
	scriptOutput, err := scriptCmd.CombinedOutput()
	if err != nil {
		r.logf("Failed to create script: %v\nOutput: %s", err, string(scriptOutput))
		return err
	}
	
	r.logf("Created script to run command with environment variables:\n%s", scriptContent)
	
	// Run the script
	cmdParts := []string{"exec", r.Config.VMName, "--", scriptPath}
	cmd := exec.Command("multipass", cmdParts...)
	
	cmd.Stdin = strings.NewReader(r.Config.StdinInput)
	cmd.Stdout = io.MultiWriter(&r.stdout, r.Logger)
	cmd.Stderr = io.MultiWriter(&r.stderr, r.Logger)

	// Run with timeout
	err = r.runWithTimeout(cmd)

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
