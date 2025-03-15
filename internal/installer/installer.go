package installer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"infinity-metrics-installer/internal/config"
	"infinity-metrics-installer/internal/docker"
	"infinity-metrics-installer/internal/git"
	"infinity-metrics-installer/internal/logging"
	"infinity-metrics-installer/internal/system"
)

// Option is a functional option for configuring the installer
type Option func(*Installer)

// Installer handles the installation process
type Installer struct {
	logger     *logging.Logger
	configFile string
	config     *config.Config
	docker     *docker.Docker
	gitManager *git.Manager
	system     *system.System
	installDir string
}

// WithLogger sets the logger for the installer
func WithLogger(logger *logging.Logger) Option {
	return func(i *Installer) {
		i.logger = logger
	}
}

// WithConfigFile sets a configuration file path
func WithConfigFile(path string) Option {
	return func(i *Installer) {
		i.configFile = path
	}
}

// WithInstallDir sets the installation directory
func WithInstallDir(dir string) Option {
	return func(i *Installer) {
		i.installDir = dir
	}
}

// NewInstaller creates a new installer instance
func NewInstaller(options ...Option) *Installer {
	// Create default installer
	i := &Installer{
		logger:     logging.NewLogger(logging.Config{Level: "info"}),
		installDir: "/opt/infinity-metrics",
		config:     config.NewConfig(),
	}

	// Apply options
	for _, option := range options {
		option(i)
	}

	// Initialize components with the logger
	i.docker = docker.NewDocker(docker.WithLogger(i.logger))
	i.gitManager = git.NewManager(git.WithLogger(i.logger))
	i.system = system.NewSystem(system.WithLogger(i.logger))

	return i
}

// Run executes the installation process
func (i *Installer) Run() error {
	// Defining total number of steps for progress tracking
	totalSteps := 7

	// 1. Check if running as root
	i.logger.Step(1, totalSteps, "Checking system privileges")
	if os.Geteuid() != 0 {
		return fmt.Errorf("this installer must be run as root")
	}

	// 2. Install dependencies
	i.logger.Step(2, totalSteps, "Installing required dependencies")
	if err := i.system.InstallDependencies(); err != nil {
		return fmt.Errorf("failed to install dependencies: %w", err)
	}

	// 3. Install Docker if needed
	i.logger.Step(3, totalSteps, "Setting up Docker")
	if err := i.docker.EnsureInstalled(); err != nil {
		return fmt.Errorf("failed to install Docker: %w", err)
	}

	// 4. Initialize Docker Swarm
	i.logger.Step(4, totalSteps, "Initializing Docker Swarm")
	if err := i.docker.InitializeSwarm(); err != nil {
		return fmt.Errorf("failed to initialize Docker Swarm: %w", err)
	}

	// 5. Setup installation directory
	i.logger.Step(5, totalSteps, "Setting up installation directory")
	i.logger.Info("Installing to %s", i.installDir)
	if err := i.system.CreateInstallDir(i.installDir); err != nil {
		return fmt.Errorf("failed to create installation directory: %w", err)
	}

	// 6. Clone or update repo
	i.logger.Step(6, totalSteps, "Setting up repository")
	if err := i.gitManager.SetupRepository(i.installDir); err != nil {
		return fmt.Errorf("failed to setup repository: %w", err)
	}

	// Process configuration
	if i.configFile != "" {
		i.logger.Info("Loading configuration from %s", i.configFile)
		if err := i.config.LoadFromFile(i.configFile); err != nil {
			return fmt.Errorf("failed to load configuration: %w", err)
		}
	} else {
		i.logger.Info("Collecting configuration from user")
		if err := i.config.CollectFromUser(); err != nil {
			return fmt.Errorf("failed to collect configuration: %w", err)
		}
	}

	i.logger.Info("Saving configuration")
	if err := i.config.SaveToFile(filepath.Join(i.installDir, ".env")); err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	// Setup cron job for updates
	i.logger.Info("Setting up automated updates")
	if err := i.system.SetupCronJob(i.installDir); err != nil {
		return fmt.Errorf("failed to setup cron job: %w", err)
	}

	// 7. Deploy stack
	i.logger.Step(7, totalSteps, "Deploying Infinity Metrics")
	if err := i.docker.DeployStack(i.installDir, i.config); err != nil {
		return fmt.Errorf("failed to deploy stack: %w", err)
	}

	// Display stack status
	cmd := exec.Command("docker", "stack", "ps", "infinity-metrics")
	output, err := cmd.CombinedOutput()
	if err == nil {
		i.logger.Info("Current stack status:")
		for _, line := range strings.Split(string(output), "\n") {
			if line != "" {
				i.logger.Info("%s", line)
			}
		}
	}

	i.logger.Success("Installation complete!")
	domain := i.config.GetString("DOMAIN", "localhost")
	i.logger.Info("You can access Infinity Metrics at: https://%s", domain)
	i.logger.Info("Installation directory: %s", i.installDir)

	return nil
}
