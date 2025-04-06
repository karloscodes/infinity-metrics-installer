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
	DefaultCronSchedule = "0 3 * * *"
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
	totalSteps := 6

	i.logger.Info("Step 1/%d: Checking system privileges", totalSteps)
	if os.Geteuid() != 0 && os.Getenv("ENV") != "test" {
		return fmt.Errorf("this installer must be run as root")
	}

	i.logger.Info("Step 2/%d: Setting up SQLite", totalSteps)
	i.logger.Info("Installing SQLite...")
	if err := i.database.EnsureSQLiteInstalled(); err != nil {
		i.logger.Error("SQLite installation failed: %v", err)
		return fmt.Errorf("failed to install SQLite: %w", err)
	}
	i.logger.Success("SQLite installed successfully")

	i.logger.Info("Step 3/%d: Setting up Docker", totalSteps)
	i.logger.Info("Installing Docker...")
	if err := i.docker.EnsureInstalled(); err != nil {
		i.logger.Error("Docker installation failed: %v", err)
		return fmt.Errorf("failed to install Docker: %w", err)
	}
	i.logger.Success("Docker installed successfully")

	i.logger.Info("Step 4/%d: Configuring Infinity Metrics", totalSteps)
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

	i.logger.Info("Fetching server configuration...")
	if err := i.config.FetchFromServer(""); err != nil {
		i.logger.Warn("Using defaults due to server config fetch failure: %v", err)
	} else {
		i.logger.Success("Server configuration fetched")
	}

	if err := i.config.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	i.logger.Info("Step 5/%d: Deploying Infinity Metrics", totalSteps)
	i.logger.Info("Deploying Docker containers...")
	if err := i.docker.Deploy(i.config); err != nil {
		i.logger.Error("Deployment failed: %v", err)
		return fmt.Errorf("failed to deploy: %w", err)
	}
	i.logger.Success("Deployment completed")

	i.logger.Info("Step 6/%d: Creating default admin user", totalSteps)
	if err := i.createDefaultUser(); err != nil {
		i.logger.Error("Default user creation failed: %v", err)
		return fmt.Errorf("failed to create default user: %w", err)
	}
	i.logger.Success("Default admin user created successfully")

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
	i.logger.Info("Restoring database...")
	err := i.database.RestoreDatabase(mainDBPath, backupDir)
	if err != nil {
		i.logger.Error("Restore failed: %v", err)
		return fmt.Errorf("failed to restore database: %w", err)
	}
	i.logger.Success("Database restored successfully")
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

func (i *Installer) createDefaultUser() error {
	i.logger.InfoWithTime("Creating default admin user")

	data := i.config.GetData()

	// Use the correct path to infinity-metrics-ctl in the container
	err := i.docker.ExecuteCommand(
		"/app/infinity-metrics-ctl", "setup-initial-user",
		data.AdminEmail,
		data.AdminPassword)
	if err != nil {
		return fmt.Errorf("failed to create admin user: %w", err)
	}

	i.logger.Success("Admin user created with email: %s", data.AdminEmail)
	return nil
}

func (i *Installer) setupCronJob() error {
	if os.Getenv("ENV") == "test" {
		i.logger.InfoWithTime("Skipping cron setup in test environment")
		return nil
	}

	cronFile := DefaultCronFile

	// Create a more robust cron job with better environment setup
	cronContent := fmt.Sprintf("# Infinity Metrics automated updates\n")
	cronContent += fmt.Sprintf("SHELL=/bin/bash\n")
	cronContent += fmt.Sprintf("PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\n")
	cronContent += fmt.Sprintf("INSTALL_DIR=%s\n", DefaultInstallDir)
	cronContent += fmt.Sprintf("%s root cd %s && %s update > %s/logs/updater.log 2>&1\n",
		DefaultCronSchedule,
		DefaultInstallDir,
		i.binaryPath,
		DefaultInstallDir)

	i.logger.Info("Setting up cron job...")

	// Ensure the logs directory exists
	logsDir := filepath.Join(DefaultInstallDir, "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		i.logger.Warn("Failed to create logs directory: %v", err)
	}

	if err := os.WriteFile(cronFile, []byte(cronContent), 0o644); err != nil {
		i.logger.Error("Cron setup failed: %v", err)
		return fmt.Errorf("failed to write cron file %s: %w", cronFile, err)
	}

	i.logger.Success("Cron job setup complete")
	i.logger.InfoWithTime("Automatic updates scheduled for 3:00 AM daily")
	return nil
}
