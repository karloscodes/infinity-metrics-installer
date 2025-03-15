package docker

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"infinity-metrics-installer/internal/config"
	"infinity-metrics-installer/internal/logging"
)

// Option is a functional option for configuring Docker
type Option func(*Docker)

// Docker manages Docker operations
type Docker struct {
	logger *logging.Logger
}

// WithLogger sets the logger for Docker operations
func WithLogger(logger *logging.Logger) Option {
	return func(d *Docker) {
		d.logger = logger
	}
}

// NewDocker creates a new Docker manager
func NewDocker(options ...Option) *Docker {
	// Create with default logger
	d := &Docker{
		logger: logging.NewLogger(logging.Config{Level: "info"}),
	}

	// Apply options
	for _, option := range options {
		option(d)
	}

	return d
}

// runCommand executes a command and returns its output and error
func (d *Docker) runCommand(name string, args ...string) (string, error) {
	d.logger.Debug("Running command: %s %s", name, strings.Join(args, " "))

	var stdout, stderr bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

// runShellCommand executes a shell command and returns its output and error
func (d *Docker) runShellCommand(command string) (string, error) {
	d.logger.Debug("Running shell command: %s", command)

	var stdout, stderr bytes.Buffer
	cmd := exec.Command("bash", "-c", command)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

// EnsureInstalled makes sure Docker is installed
func (d *Docker) EnsureInstalled() error {
	// Check if Docker is already installed
	_, err := d.runCommand("docker", "--version")
	if err == nil {
		d.logger.Success("Docker is already installed")
		return nil
	}

	d.logger.Info("Docker not found, installing using official convenience script...")

	// Install Docker using the official convenience script
	installCmd := "curl -fsSL https://get.docker.com | sh"
	start := time.Now()
	output, err := d.runShellCommand(installCmd)
	if err != nil {
		return fmt.Errorf("failed to install Docker: %w", err)
	}
	elapsed := time.Since(start).Round(time.Second)
	d.logger.Debug("Docker installation completed in %s", elapsed)
	d.logger.Debug("Installation output: %s", output)

	// Start and enable Docker service
	d.logger.Info("Enabling Docker service")
	_, err = d.runShellCommand("systemctl start docker && systemctl enable docker")
	if err != nil {
		return fmt.Errorf("failed to enable Docker service: %w", err)
	}

	// Verify installation
	version, err := d.runCommand("docker", "--version")
	if err != nil {
		return fmt.Errorf("Docker installation verification failed: %w", err)
	}

	d.logger.Success("Docker installed successfully: %s", strings.TrimSpace(version))
	return nil
}

// InitializeSwarm initializes Docker Swarm
func (d *Docker) InitializeSwarm() error {
	// Check if already in swarm mode
	d.logger.Info("Checking Docker Swarm status")
	info, err := d.runCommand("docker", "info")
	if err != nil {
		return fmt.Errorf("failed to get Docker info: %w", err)
	}

	if strings.Contains(info, "Swarm: active") {
		d.logger.Success("Docker Swarm is already active")
		return nil
	}

	// Get public IP address
	d.logger.Info("Getting public IP address")
	ipOutput, err := d.runCommand("curl", "-s", "ifconfig.me")
	if err != nil {
		return fmt.Errorf("failed to get public IP: %w", err)
	}
	publicIP := strings.TrimSpace(ipOutput)
	d.logger.Debug("Public IP: %s", publicIP)

	// Initialize swarm
	d.logger.Info("Initializing Docker Swarm")
	output, err := d.runCommand("docker", "swarm", "init", "--advertise-addr", publicIP)
	if err != nil {
		return fmt.Errorf("failed to initialize swarm: %w", err)
	}

	d.logger.Success("Docker Swarm initialized successfully")
	d.logger.Debug("Swarm init output: %s", strings.TrimSpace(output))

	return nil
}

// DeployStack deploys the Docker stack
func (d *Docker) DeployStack(installDir string, conf *config.Config) error {
	deploymentDir := filepath.Join(installDir, "deployment")
	envFile := filepath.Join(installDir, ".env")

	// Process docker-compose.yml with environment variables from .env file
	d.logger.Info("Preparing stack configuration")

	// Create docker-compose config command with env file
	configCmd := exec.Command("docker compose", "--env-file", envFile, "config")
	configCmd.Dir = deploymentDir

	// Execute and capture output
	var configOutput bytes.Buffer
	configCmd.Stdout = &configOutput

	var configError bytes.Buffer
	configCmd.Stderr = &configError

	if err := configCmd.Run(); err != nil {
		return fmt.Errorf("failed to process configuration: %w - %s", err, configError.String())
	}

	// Write processed config to a temporary file
	tempStackFile := filepath.Join(deploymentDir, "stack.yml")
	if err := os.WriteFile(tempStackFile, configOutput.Bytes(), 0o644); err != nil {
		return fmt.Errorf("failed to write stack file: %w", err)
	}
	d.logger.Debug("Stack configuration processed and saved to %s", tempStackFile)

	// Deploy stack with processed config
	d.logger.Info("Deploying Docker stack")
	deployCmd := exec.Command(
		"docker", "stack", "deploy",
		"-c", tempStackFile,
		"infinity-metrics",
		"--prune",
	)
	deployCmd.Dir = deploymentDir

	var stdout, stderr bytes.Buffer
	deployCmd.Stdout = &stdout
	deployCmd.Stderr = &stderr

	err := deployCmd.Run()
	if err != nil {
		return fmt.Errorf("failed to deploy stack: %w - %s", err, stderr.String())
	}

	d.logger.Success("Stack deployed successfully")
	if stdout.Len() > 0 {
		for _, line := range strings.Split(stdout.String(), "\n") {
			if line != "" {
				d.logger.Debug("%s", line)
			}
		}
	}

	return nil
}

// CheckDockerStatus returns the status of Docker components
func (d *Docker) CheckDockerStatus() (map[string]string, error) {
	status := make(map[string]string)

	// Check Docker version
	version, err := d.runCommand("docker", "--version")
	if err != nil {
		status["docker"] = "not installed"
	} else {
		status["docker"] = strings.TrimSpace(version)
	}

	// Check Swarm status
	info, err := d.runCommand("docker", "info")
	if err != nil {
		status["swarm"] = "unknown"
	} else if strings.Contains(info, "Swarm: active") {
		status["swarm"] = "active"
	} else {
		status["swarm"] = "inactive"
	}

	// Check if infinity-metrics stack is deployed
	stacks, err := d.runCommand("docker", "stack", "ls")
	if err != nil {
		status["stack"] = "unknown"
	} else if strings.Contains(stacks, "infinity-metrics") {
		status["stack"] = "deployed"
	} else {
		status["stack"] = "not deployed"
	}

	return status, nil
}
