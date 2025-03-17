package installer

import (
	"fmt"
	"os"
	"path/filepath"

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

// Run executes the installation process
func (i *Installer) Run() error {
	totalSteps := 5

	i.logger.Step(1, totalSteps, "Checking system privileges")
	if os.Geteuid() != 0 && os.Getenv("ENV") != "test" {
		return fmt.Errorf("this installer must be run as root")
	}

	i.logger.Step(2, totalSteps, "Setting up SQLite")
	if err := i.database.EnsureSQLiteInstalled(); err != nil {
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

	if err := i.config.FetchFromServer(""); err != nil {
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
	if err := i.setupCronJob(); err != nil {
		return fmt.Errorf("failed to setup cron: %w", err)
	}

	i.logger.Success("Installation complete!")
	i.logger.Info("Access your dashboard at https://%s", data.Domain)
	i.logger.Info("Installation directory: %s", data.InstallDir)
	return nil
}

// Restore handles the database restore process
func (i *Installer) Restore() error {
	backupDir := i.GetBackupDir()
	mainDBPath := i.GetMainDBPath()

	return i.database.RestoreDatabase(mainDBPath, backupDir)
}

func (i *Installer) createInstallDir(installDir string) error {
	i.logger.Info("Creating installation directory: %s", installDir)
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	i.logger.Success("Installation directory created")
	return nil
}

func (i *Installer) setupCronJob() error {
	cronFile := "/etc/cron.d/infinity-metrics-update"
	binaryPath := "/usr/local/bin/infinity-metrics" // Match script’s install location
	cronContent := fmt.Sprintf("0 0 * * * root %s update\n", binaryPath)

	if err := os.WriteFile(cronFile, []byte(cronContent), 0o644); err != nil {
		return fmt.Errorf("failed to write cron file: %w", err)
	}
	i.logger.Success("Automatic updates scheduled for midnight")
	return nil
}
