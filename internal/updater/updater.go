package updater

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"infinity-metrics-installer/internal/config"
	"infinity-metrics-installer/internal/database"
	"infinity-metrics-installer/internal/docker"
	"infinity-metrics-installer/internal/logging"
)

type Updater struct {
	logger   *logging.Logger
	config   *config.Config
	docker   *docker.Docker
	database *database.Database
}

func NewUpdater(logger *logging.Logger) *Updater {
	db := database.NewDatabase(logger)
	return &Updater{
		logger:   logger,
		config:   config.NewConfig(logger),
		docker:   docker.NewDocker(logger, db),
		database: db,
	}
}

func (u *Updater) Run() error {
	totalSteps := 4

	u.logger.Step(1, totalSteps, "Ensuring SQLite is installed")
	if err := u.ensureSQLiteInstalled(); err != nil {
		u.logger.Warn("SQLite installation failed: %v", err)
		u.logger.Warn("Proceeding with limited backup capabilities")
	}

	u.logger.Step(2, totalSteps, "Loading configuration")
	data := u.config.GetData()
	envFile := filepath.Join(data.InstallDir, ".env")
	if err := u.config.LoadFromFile(envFile); err != nil {
		return fmt.Errorf("failed to load config from %s: %w", envFile, err)
	}

	u.logger.Step(3, totalSteps, "Checking for updates from server")
	if err := u.config.FetchFromServer("https://getinfinitymetrics.com/config.json"); err != nil {
		u.logger.Warn("Server config fetch failed, using local config: %v", err)
	}

	u.logger.Step(4, totalSteps, "Applying updates")
	// Create backup before update
	mainDBPath := u.GetMainDBPath()
	backupDir := u.GetBackupDir()
	if _, err := u.database.BackupDatabase(mainDBPath, backupDir); err != nil {
		u.logger.Warn("Failed to backup database before update: %v", err)
		u.logger.Warn("Proceeding with update without backup")
	} else {
		u.logger.Success("Database backup created successfully")
	}

	// Update Docker containers
	if err := u.docker.Update(u.config); err != nil {
		return fmt.Errorf("failed to update Docker containers: %w", err)
	}

	if err := u.config.SaveToFile(envFile); err != nil {
		return fmt.Errorf("failed to save config to %s: %w", envFile, err)
	}

	u.logger.Success("Update completed successfully")
	return nil
}

// GetMainDBPath returns the path to the main database file
func (u *Updater) GetMainDBPath() string {
	data := u.config.GetData()
	return filepath.Join(data.InstallDir, "storage", "infinity-metrics-production.db")
}

// GetBackupDir returns the path to the backup directory
func (u *Updater) GetBackupDir() string {
	data := u.config.GetData()
	return filepath.Join(data.InstallDir, "storage", "backups")
}

// ensureSQLiteInstalled installs SQLite if not already available
func (u *Updater) ensureSQLiteInstalled() error {
	u.logger.Info("Checking for SQLite installation...")

	// Try to run sqlite3 --version to check if it's installed
	cmd := exec.Command("sqlite3", "--version")
	if err := cmd.Run(); err == nil {
		u.logger.Success("SQLite is already installed")
		return nil
	}

	// SQLite is not installed, install it using apt-get (assuming Debian/Ubuntu)
	u.logger.Info("Installing SQLite...")
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

	u.logger.Success("SQLite installed successfully")
	return nil
}
