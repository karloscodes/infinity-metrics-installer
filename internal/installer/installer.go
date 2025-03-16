package installer

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"infinity-metrics-installer/internal/config"
	"infinity-metrics-installer/internal/database"
	"infinity-metrics-installer/internal/docker"
	"infinity-metrics-installer/internal/logging"
)

// Installer handles the installation and update process
type Installer struct {
	logger   *logging.Logger
	config   *config.Config
	docker   *docker.Docker
	database *database.Database
}

// NewInstaller creates a new installer instance
func NewInstaller(logger *logging.Logger) *Installer {
	db := database.NewDatabase(logger)
	d := docker.NewDocker(logger, db)
	return &Installer{
		logger:   logger,
		config:   config.NewConfig(logger),
		docker:   d,
		database: db,
	}
}

// GetConfig returns the installer's configuration
func (i *Installer) GetConfig() *config.Config {
	return i.config
}

// GetMainDBPath returns the path to the main database file
func (i *Installer) GetMainDBPath() string {
	data := i.config.GetData()
	return filepath.Join(data.InstallDir, "storage", "infinity-metrics-production.db")
}

// GetBackupDir returns the path to the backup directory
func (i *Installer) GetBackupDir() string {
	data := i.config.GetData()
	return filepath.Join(data.InstallDir, "storage", "backups")
}

// EnsureSQLiteInstalled installs SQLite if not already available
func (i *Installer) EnsureSQLiteInstalled() error {
	i.logger.Info("Checking for SQLite installation...")

	// Try to run sqlite3 --version to check if it's installed
	cmd := exec.Command("sqlite3", "--version")
	if err := cmd.Run(); err == nil {
		i.logger.Success("SQLite is already installed")
		return nil
	}

	// SQLite is not installed, install it using apt-get (assuming Debian/Ubuntu)
	i.logger.Info("Installing SQLite...")
	if os.Geteuid() != 0 {
		return fmt.Errorf("must run as root to install SQLite")
	}

	// Update package list
	updateCmd := exec.Command("apt-get", "update", "-y")
	if err := updateCmd.Run(); err != nil {
		return fmt.Errorf("failed to update package list: %w", err)
	}

	// Install sqlite3
	installCmd := exec.Command("apt-get", "install", "-y", "sqlite3")
	if err := installCmd.Run(); err != nil {
		return fmt.Errorf("failed to install SQLite: %w", err)
	}

	// Verify installation
	verifyCmd := exec.Command("sqlite3", "--version")
	if err := verifyCmd.Run(); err != nil {
		return fmt.Errorf("SQLite installation verification failed: %w", err)
	}

	i.logger.Success("SQLite installed successfully")
	return nil
}

// Run executes the installation process
func (i *Installer) Run() error {
	totalSteps := 5

	i.logger.Step(1, totalSteps, "Checking system privileges")
	if os.Geteuid() != 0 {
		return fmt.Errorf("this installer must be run as root")
	}

	i.logger.Step(2, totalSteps, "Setting up SQLite")
	if err := i.EnsureSQLiteInstalled(); err != nil {
		return fmt.Errorf("failed to install SQLite: %w", err)
	}

	i.logger.Step(3, totalSteps, "Setting up Docker")
	if err := i.docker.EnsureInstalled(); err != nil {
		return fmt.Errorf("failed to install Docker: %w", err)
	}

	i.logger.Step(4, totalSteps, "Configuring Infinity Metrics")
	data := i.config.GetData()
	if err := i.createInstallDir(data.InstallDir); err != nil {
		return fmt.Errorf("failed to create install dir: %w", err)
	}
	envFile := filepath.Join(data.InstallDir, ".env")
	if _, err := os.Stat(envFile); os.IsNotExist(err) {
		i.logger.Info("Collecting configuration from user")
		if err := i.config.CollectFromUser(); err != nil {
			return fmt.Errorf("failed to collect config: %w", err)
		}
		if err := i.config.SaveToFile(envFile); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}
	} else {
		i.logger.Info("Loading configuration from %s", envFile)
		if err := i.config.LoadFromFile(envFile); err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
	}

	if err := i.config.FetchFromServer("https://getinfinitymetrics.com/config.json"); err != nil {
		i.logger.Warn("Server config fetch failed, using defaults: %v", err)
	}
	if err := i.config.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	i.logger.Step(5, totalSteps, "Deploying Infinity Metrics")
	if err := i.docker.Deploy(i.config); err != nil {
		return fmt.Errorf("failed to deploy: %w", err)
	}

	i.logger.Info("Setting up automated updates")
	if err := i.setupCronJob(data.InstallDir); err != nil {
		return fmt.Errorf("failed to setup cron: %w", err)
	}

	i.logger.Success("Installation complete!")
	i.logger.Info("Access your dashboard at https://%s", data.Domain)
	i.logger.Info("Installation directory: %s", data.InstallDir)
	return nil
}

// Update runs the update process
func (i *Installer) Update(currentVersion string) error {
	data := i.config.GetData()
	envFile := filepath.Join(data.InstallDir, ".env")

	if err := i.config.LoadFromFile(envFile); err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := i.config.FetchFromServer("https://getinfinitymetrics.com/config.json"); err != nil {
		i.logger.Warn("Server config fetch failed, using local: %v", err)
	}

	// Download new binary on every update
	arch := runtime.GOARCH // "amd64" or "arm64"
	if arch != "amd64" && arch != "arm64" {
		return fmt.Errorf("unsupported architecture: %s", arch)
	}
	if data.InstallerURL != "" {
		if err := i.updateBinary(data.InstallerURL, data.InstallDir, arch); err != nil {
			i.logger.Warn("Failed to update binary: %v", err)
		} else {
			i.logger.Success("Binary updated to version %s, restarting", data.InstallerVersion)
			return exec.Command(filepath.Join(data.InstallDir, "infinity-metrics"), "update").Run()
		}
	}

	if err := i.docker.Update(i.config); err != nil {
		return fmt.Errorf("update failed: %w", err)
	}
	if err := i.config.SaveToFile(envFile); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	i.logger.Success("Update completed")
	return nil
}

// Restore handles the database restore process
func (i *Installer) Restore() error {
	backupDir := i.GetBackupDir()
	mainDBPath := i.GetMainDBPath()

	// Ensure SQLite is installed
	if err := i.EnsureSQLiteInstalled(); err != nil {
		return fmt.Errorf("ensure SQLite installed: %w", err)
	}

	// Ensure backup directory exists
	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		return fmt.Errorf("backup directory not found: %s", backupDir)
	}

	// List backups
	backups, err := i.database.ListBackups(backupDir)
	if err != nil {
		return fmt.Errorf("list backups: %w", err)
	}
	if len(backups) == 0 {
		return fmt.Errorf("no backups found in %s", backupDir)
	}

	// Prompt user to select a backup
	backupPath, err := i.database.PromptSelection(backups)
	if err != nil {
		return fmt.Errorf("selection failed: %w", err)
	}

	// Restore the database
	currentName := "infinity-app-1"
	if !i.docker.IsRunning(currentName) {
		currentName = "infinity-app-2"
		if !i.docker.IsRunning(currentName) {
			currentName = ""
		}
	}
	if currentName != "" {
		i.logger.Info("Stopping container %s for restore", currentName)
		i.docker.StopAndRemove(currentName)
	}

	if err := i.database.RestoreDatabase(mainDBPath, backupPath); err != nil {
		if currentName != "" {
			i.logger.Warn("Restarting original container due to restore failure")
			i.docker.DeployApp(i.config.GetData(), currentName)
		}
		return fmt.Errorf("restore operation failed: %w", err)
	}

	// Restart container if it was running
	if currentName != "" {
		i.logger.Info("Restarting container %s", currentName)
		if err := i.docker.DeployApp(i.config.GetData(), currentName); err != nil {
			return fmt.Errorf("restart container: %w", err)
		}
	}

	return nil
}

func (i *Installer) createInstallDir(installDir string) error {
	i.logger.Info("Creating installation directory: %s", installDir)
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	i.logger.Success("Installation directory created")
	return nil
}

func (i *Installer) setupCronJob(installDir string) error {
	cronFile := "/etc/cron.d/infinity-metrics-update"
	binaryPath := filepath.Join(installDir, "infinity-metrics")
	cronContent := fmt.Sprintf("0 0 * * * root %s update\n", binaryPath)

	if err := os.WriteFile(cronFile, []byte(cronContent), 0o644); err != nil {
		return fmt.Errorf("failed to write cron file: %w", err)
	}
	i.logger.Success("Automatic updates scheduled for midnight")
	return nil
}

func (i *Installer) updateBinary(url, installDir, arch string) error {
	downloadURL := fmt.Sprintf("%s-%s", url, arch)
	resp, err := http.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	newBinary := filepath.Join(installDir, "infinity-metrics.new")
	out, err := os.Create(newBinary)
	if err != nil {
		return fmt.Errorf("create new binary: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		return fmt.Errorf("write new binary: %w", err)
	}

	if err := os.Chmod(newBinary, 0o755); err != nil {
		return fmt.Errorf("chmod new binary: %w", err)
	}

	// Replace old binary
	oldBinary := filepath.Join(installDir, "infinity-metrics")
	if err := os.Rename(newBinary, oldBinary); err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}

	return nil
}
