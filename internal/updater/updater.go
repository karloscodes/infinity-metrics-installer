package updater

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"infinity-metrics-installer/internal/config"
	"infinity-metrics-installer/internal/docker"
	"infinity-metrics-installer/internal/logging"
)

// Option is a functional option for configuring the updater
type Option func(*Updater)

// Updater handles the update process
type Updater struct {
	logger      *logging.Logger
	forceUpdate bool
	backup      bool
	configFile  string
	installDir  string
	config      *config.Config
	docker      *docker.Docker
}

// WithLogger sets the logger for the updater
func WithLogger(logger *logging.Logger) Option {
	return func(u *Updater) {
		u.logger = logger
	}
}

// WithForceUpdate sets whether to force update even if no changes
func WithForceUpdate(force bool) Option {
	return func(u *Updater) {
		u.forceUpdate = force
	}
}

// WithBackup sets whether to perform database backup
func WithBackup(backup bool) Option {
	return func(u *Updater) {
		u.backup = backup
	}
}

// WithInstallDir sets the installation directory
func WithInstallDir(dir string) Option {
	return func(u *Updater) {
		u.installDir = dir
	}
}

// WithConfigFile sets a specific config file to use
func WithConfigFile(file string) Option {
	return func(u *Updater) {
		u.configFile = file
	}
}

// NewUpdater creates a new updater instance
func NewUpdater(options ...Option) *Updater {
	// Get current working directory as default install dir
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "/opt/infinity-metrics"
	}

	// Create default updater
	u := &Updater{
		logger:      logging.NewLogger(logging.Config{Level: "info"}),
		forceUpdate: false,
		backup:      true,
		installDir:  cwd,
		config:      config.NewConfig(),
	}

	// Apply options
	for _, option := range options {
		option(u)
	}

	// Initialize Docker with the logger
	u.docker = docker.NewDocker(docker.WithLogger(u.logger))

	return u
}

// Run executes the update process
func (u *Updater) Run() error {
	// Defining total number of steps for progress tracking
	totalSteps := 5

	// 1. Detect and load environment
	u.logger.Step(1, totalSteps, "Checking environment")

	deploymentDir := filepath.Join(u.installDir, "deployment")

	// Load environment variables from installation directory
	envFile := filepath.Join(deploymentDir, ".env")
	if u.configFile != "" {
		envFile = u.configFile
	}

	if _, err := os.Stat(envFile); err == nil {
		u.logger.Info("Loading configuration from %s", envFile)
		if err := u.config.LoadFromFile(envFile); err != nil {
			return fmt.Errorf("failed to load configuration: %w", err)
		}
	} else {
		u.logger.Warn("No .env file found at %s, using defaults", envFile)
	}

	// 2. Create database backup if enabled
	u.logger.Step(2, totalSteps, "Creating database backup")
	if u.backup {
		if err := u.createBackup(); err != nil {
			u.logger.Warn("Backup creation failed: %s", err)
			// Continue with update even if backup fails, but log a warning
		}
	} else {
		u.logger.Info("Database backup skipped (disabled)")
	}

	// 3. Pull git repository updates
	u.logger.Step(3, totalSteps, "Updating repository")
	if err := u.updateRepository(); err != nil {
		return fmt.Errorf("failed to update repository: %w", err)
	}

	// 5. Deploy the updated stack
	u.logger.Step(5, totalSteps, "Deploying updated stack")
	if err := u.deployStack(); err != nil {
		return fmt.Errorf("failed to deploy stack: %w", err)
	}

	// Verify and display results
	u.logger.Success("Update completed successfully")

	// Show stack status
	cmd := exec.Command("docker", "stack", "ps", "infinity-metrics")
	output, err := cmd.CombinedOutput()
	if err == nil {
		u.logger.Info("Current stack status:")
		for _, line := range strings.Split(string(output), "\n") {
			if line != "" {
				u.logger.Info("%s", line)
			}
		}
	}

	return nil
}

// createBackup creates a backup of the database
func (u *Updater) createBackup() error {
	u.logger.Info("Creating database backup")

	deploymentDir := filepath.Join(u.installDir, "deployment")
	// Create backup directory if it doesn't exist
	backupDir := filepath.Join(deploymentDir, "storage", "backups")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Look for database file
	dbFile := filepath.Join(deploymentDir, "storage", "infinity-metrics-production.db")
	if _, err := os.Stat(dbFile); err != nil {
		return fmt.Errorf("database file not found: %w", err)
	}

	// Create timestamped backup filename
	timestamp := time.Now().Format("2006-01-02-150405")
	backupFile := filepath.Join(backupDir, fmt.Sprintf("backup-%s.db", timestamp))

	// Copy database file
	u.logger.Debug("Copying %s to %s", dbFile, backupFile)
	input, err := os.ReadFile(dbFile)
	if err != nil {
		return fmt.Errorf("failed to read database file: %w", err)
	}

	if err := os.WriteFile(backupFile, input, 0o644); err != nil {
		return fmt.Errorf("failed to write backup file: %w", err)
	}

	u.logger.Success("Database backup created: %s", backupFile)

	// Clean up old backups (keep last 14)
	u.cleanupOldBackups(backupDir)

	return nil
}

// cleanupOldBackups removes older backups, keeping the most recent ones
func (u *Updater) cleanupOldBackups(backupDir string) {
	u.logger.Debug("Cleaning up old backups")

	// List all backup files
	files, err := filepath.Glob(filepath.Join(backupDir, "backup-*.db"))
	if err != nil {
		u.logger.Warn("Failed to list backup files: %s", err)
		return
	}

	// If we have more than 14 backups, delete the oldest ones
	if len(files) > 14 {
		// Sort files by modification time
		type fileInfo struct {
			path string
			time time.Time
		}

		fileInfos := make([]fileInfo, 0, len(files))
		for _, file := range files {
			info, err := os.Stat(file)
			if err != nil {
				continue
			}
			fileInfos = append(fileInfos, fileInfo{path: file, time: info.ModTime()})
		}

		// Sort by time (oldest first)
		for i := 0; i < len(fileInfos); i++ {
			for j := i + 1; j < len(fileInfos); j++ {
				if fileInfos[i].time.After(fileInfos[j].time) {
					fileInfos[i], fileInfos[j] = fileInfos[j], fileInfos[i]
				}
			}
		}

		// Delete the oldest ones
		for i := 0; i < len(fileInfos)-14; i++ {
			u.logger.Debug("Removing old backup: %s", fileInfos[i].path)
			if err := os.Remove(fileInfos[i].path); err != nil {
				u.logger.Warn("Failed to remove old backup %s: %s", fileInfos[i].path, err)
			}
		}
	}
}

// updateRepository pulls the latest changes from the git repository
func (u *Updater) updateRepository() error {
	u.logger.Info("Pulling latest repository changes")

	// Check if this directory is a git repository
	gitDir := filepath.Join(u.installDir, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		return fmt.Errorf("not a git repository: %s", u.installDir)
	}

	// Run git pull
	cmd := exec.Command("git", "pull")
	cmd.Dir = u.installDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git pull failed: %s, %w", string(output), err)
	}

	// Check if there were any changes
	if strings.Contains(string(output), "Already up to date") && !u.forceUpdate {
		u.logger.Info("No updates available")
	} else {
		u.logger.Success("Repository updated successfully")
	}

	return nil
}

// deployStack deploys the updated stack
func (u *Updater) deployStack() error {
	u.logger.Info("Deploying stack with zero-downtime updates")

	// Deploy the stack
	composeFile := filepath.Join(u.installDir, "docker-compose.yml")
	if _, err := os.Stat(composeFile); err != nil {
		return fmt.Errorf("docker-compose.yml not found: %s", composeFile)
	}

	cmd := exec.Command(
		"docker", "stack", "deploy",
		"-c", composeFile,
		"infinity-metrics",
		"--env-file", filepath.Join(u.installDir, ".env"),
		"--prune",
	)
	cmd.Dir = u.installDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("stack deployment failed: %s, %w", string(output), err)
	}

	u.logger.Success("Stack deployed successfully")
	return nil
}
