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

const (
	DefaultInstallDir   = "/opt/infinity-metrics"
	DefaultBinaryPath   = "/usr/local/bin/infinity-metrics"
	DefaultCronFile     = "/etc/cron.d/infinity-metrics-update"
	DefaultCronSchedule = "0 0 * * *"
)

type Installer struct {
	logger     *logging.Logger
	config     *config.Config
	docker     *docker.Docker
	database   *database.Database
	binaryPath string
}

func NewInstaller(logger *logging.Logger) *Installer {
	db := database.NewDatabase(logger)
	d := docker.NewDocker(logger, db)
	return &Installer{
		logger:     logger,
		config:     config.NewConfig(logger),
		docker:     d,
		database:   db,
		binaryPath: DefaultBinaryPath,
	}
}

func (i *Installer) GetConfig() *config.Config {
	return i.config
}

func (i *Installer) GetMainDBPath() string {
	data := i.config.GetData()
	return filepath.Join(data.InstallDir, "storage", "infinity-metrics-production.db")
}

func (i *Installer) GetBackupDir() string {
	data := i.config.GetData()
	return filepath.Join(data.InstallDir, "storage", "backups")
}

func (i *Installer) RunWithConfig(cfg *config.Config) error {
	i.config = cfg
	return i.Run()
}

func (i *Installer) Run() error {
	totalSteps := 5

	i.logger.Step(1, totalSteps, "Checking system privileges")
	if os.Geteuid() != 0 && os.Getenv("ENV") != "test" {
		return fmt.Errorf("this installer must be run as root")
	}

	i.logger.Step(2, totalSteps, "Setting up SQLite")
	stop := i.logger.StartSpinner("Installing SQLite...")
	if err := i.database.EnsureSQLiteInstalled(); err != nil {
		i.logger.StopSpinner(stop, false, "SQLite installation failed")
		return fmt.Errorf("failed to install SQLite: %w", err)
	}
	i.logger.StopSpinner(stop, true, "SQLite installed successfully")

	i.logger.Step(3, totalSteps, "Setting up Docker")
	stop = i.logger.StartSpinner("Installing Docker...")
	if err := i.docker.EnsureInstalled(); err != nil {
		i.logger.StopSpinner(stop, false, "Docker installation failed")
		return fmt.Errorf("failed to install Docker: %w", err)
	}
	i.logger.StopSpinner(stop, true, "Docker installed successfully")

	i.logger.Step(4, totalSteps, "Configuring Infinity Metrics")
	data := i.config.GetData()
	if err := i.createInstallDir(data.InstallDir); err != nil {
		return fmt.Errorf("failed to create install dir: %w", err)
	}
	envFile := filepath.Join(data.InstallDir, ".env")
	if _, err := os.Stat(envFile); os.IsNotExist(err) {
		if err := i.config.SaveToFile(envFile); err != nil {
			return fmt.Errorf("failed to save config to %s: %w", envFile, err)
		}
	} else {
		i.logger.InfoWithTime("Loading existing configuration from %s", envFile)
		if err := i.config.LoadFromFile(envFile); err != nil {
			return fmt.Errorf("failed to load config from %s: %w", envFile, err)
		}
	}

	stop = i.logger.StartSpinner("Fetching server configuration...")
	if err := i.config.FetchFromServer(""); err != nil {
		i.logger.StopSpinner(stop, false, "Server config fetch failed")
		i.logger.Warn("Using defaults due to server config fetch failure: %v", err)
	} else {
		i.logger.StopSpinner(stop, true, "Server configuration fetched")
	}

	if err := i.config.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	i.logger.Step(5, totalSteps, "Deploying Infinity Metrics")
	stop = i.logger.StartSpinner("Deploying Docker containers...")
	if err := i.docker.Deploy(i.config); err != nil {
		i.logger.StopSpinner(stop, false, "Deployment failed")
		return fmt.Errorf("failed to deploy: %w", err)
	}
	i.logger.StopSpinner(stop, true, "Deployment completed")

	i.logger.InfoWithTime("Setting up automated updates")
	if err := i.setupCronJob(); err != nil {
		return fmt.Errorf("failed to setup cron: %w", err)
	}

	return nil
}

func (i *Installer) Restore() error {
	backupDir := i.GetBackupDir()
	mainDBPath := i.GetMainDBPath()

	i.logger.InfoWithTime("Restoring database from %s to %s", backupDir, mainDBPath)
	stop := i.logger.StartSpinner("Restoring database...")
	err := i.database.RestoreDatabase(mainDBPath, backupDir)
	if err != nil {
		i.logger.StopSpinner(stop, false, "Restore failed")
		return fmt.Errorf("failed to restore database: %w", err)
	}
	i.logger.StopSpinner(stop, true, "Database restored successfully")
	return nil
}

func (i *Installer) createInstallDir(installDir string) error {
	i.logger.InfoWithTime("Creating installation directory: %s", installDir)
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	i.logger.Success("Installation directory created")
	return nil
}

func (i *Installer) setupCronJob() error {
	if os.Getenv("ENV") == "test" {
		i.logger.InfoWithTime("Skipping cron setup in test environment")
		return nil
	}

	cronFile := DefaultCronFile
	cronContent := fmt.Sprintf("%s root %s update\n", DefaultCronSchedule, i.binaryPath)

	stop := i.logger.StartSpinner("Setting up cron job...")
	if err := os.WriteFile(cronFile, []byte(cronContent), 0o644); err != nil {
		i.logger.StopSpinner(stop, false, "Cron setup failed")
		return fmt.Errorf("failed to write cron file %s: %w", cronFile, err)
	}
	i.logger.StopSpinner(stop, true, "Cron job setup complete")
	i.logger.InfoWithTime("Automatic updates scheduled for midnight daily")
	return nil
}
