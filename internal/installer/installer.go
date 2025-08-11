package installer

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"infinity-metrics-installer/internal/admin"
	"infinity-metrics-installer/internal/config"
	"infinity-metrics-installer/internal/cron"
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
	logger       *logging.Logger
	config       *config.Config
	docker       *docker.Docker
	database     *database.Database
	binaryPath   string
	portWarnings []string
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
	// Step 1: Privilege check - already done in main, just confirm
	i.logger.Success("Root privileges confirmed")

	i.logger.Info("Step 2/%d: Setting up SQLite", totalSteps)
	// Step 2: SQLite
	i.logger.Info("Installing SQLite...")
	if err := i.database.EnsureSQLiteInstalled(); err != nil {
		i.logger.Error("SQLite installation failed: %v", err)
		return fmt.Errorf("failed to install SQLite: %w", err)
	}
	i.logger.Success("SQLite installed successfully")

	i.logger.Info("Step 3/%d: Setting up Docker", totalSteps)
	// Step 3: Docker
	i.logger.Info("Installing Docker...")
	// Show progress indicator for Docker installation
	progressChan := make(chan int, 1)
	go i.showProgress(progressChan, "Docker installation")
	if err := i.docker.EnsureInstalled(); err != nil {
		close(progressChan)
		i.logger.Error("Docker installation failed: %v", err)
		return fmt.Errorf("failed to install Docker: %w", err)
	}
	progressChan <- 100
	close(progressChan)
	i.logger.Success("Docker installed successfully")

	i.logger.Info("Step 4/%d: Configuring Infinity Metrics", totalSteps)
	// Step 4: Config
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
	i.logger.Success("Configuration validated and saved to %s", envFile)

	i.logger.Info("Step 5/%d: Deploying Infinity Metrics", totalSteps)
	// Step 5: Deploy
	i.logger.Info("Deploying Docker containers...")
	// Show progress indicator for deployment
	deployProgressChan := make(chan int, 1)
	go i.showProgress(deployProgressChan, "Deployment")
	if err := i.docker.Deploy(i.config); err != nil {
		close(deployProgressChan)
		i.logger.Error("Deployment failed: %v", err)
		return fmt.Errorf("failed to deploy: %w", err)
	}
	deployProgressChan <- 100
	close(deployProgressChan)
	i.logger.Success("Deployment completed")

	i.logger.Info("Step 6/%d: Creating default admin user", totalSteps)
	// Step 6: Admin user
	if err := i.createDefaultUser(); err != nil {
		i.logger.Error("Default user creation failed: %v", err)
		return fmt.Errorf("failed to create default user: %w", err)
	}
	// Install the binary itself for updates and cron jobs
	if err := i.installBinary(); err != nil {
		i.logger.Warn("Failed to install binary for updates: %v", err)
		// Don't fail installation, just warn
	}

	i.logger.InfoWithTime("Setting up automated updates")
	cronManager := cron.NewManager(i.logger)
	if err := cronManager.SetupCronJob(); err != nil {
		return fmt.Errorf("failed to setup cron: %w", err)
	}
	i.logger.Success("Daily automatic updates configured for 3:00 AM")

	return nil
}

// ListBackups returns available database backups
func (i *Installer) ListBackups() ([]database.BackupFile, error) {
	backupDir := i.GetBackupDir()
	return i.database.ListBackups(backupDir)
}

// PromptBackupSelection allows user to select from available backups
func (i *Installer) PromptBackupSelection(backups []database.BackupFile) (string, error) {
	return i.database.PromptSelection(backups)
}

// ValidateBackup validates the selected backup file
func (i *Installer) ValidateBackup(backupPath string) error {
	return i.database.ValidateBackup(backupPath)
}

// RestoreFromBackup restores database from a specific backup file
func (i *Installer) RestoreFromBackup(backupPath string) error {
	mainDBPath := i.GetMainDBPath()
	
	i.logger.InfoWithTime("Restoring database from %s to %s", backupPath, mainDBPath)
	i.logger.Info("Restoring database...")

	// Show progress for restore operation
	progressChan := make(chan int, 1)
	go i.showProgress(progressChan, "Database restore")

	err := i.database.RestoreDatabase(mainDBPath, backupPath)
	if err != nil {
		close(progressChan)
		i.logger.Error("Restore failed: %v", err)
		return fmt.Errorf("failed to restore database: %w", err)
	}

	progressChan <- 100
	close(progressChan)

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
	data := i.config.GetData()

	adminMgr := admin.NewManager(i.logger)
	if err := adminMgr.CreateAdminUser(data.AdminEmail, data.AdminPassword); err != nil {
		return fmt.Errorf("failed to create admin user: %w", err)
	}

	i.logger.Success("Admin user created with email: %s", data.AdminEmail)
	return nil
}

// VerifyInstallation provides a way to verify that the installation completed successfully
func (i *Installer) VerifyInstallation() ([]string, error) {
	var warnings []string
	// Check that Docker containers are running
	containersRunning, err := i.docker.VerifyContainersRunning()
	if err != nil {
		return warnings, fmt.Errorf("installation verification failed: %w", err)
	}
	if !containersRunning {
		return warnings, fmt.Errorf("Docker containers are not running properly")
	}
	// Check that the database exists
	dbPath := i.GetMainDBPath()
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return warnings, fmt.Errorf("database file not found: %w", err)
	}
	// Ports are now checked as hard requirements before installation
	return warnings, nil
}

// checkPort checks if a port is available
func checkPort(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

// showProgress displays a progress indicator for long-running operations
func (i *Installer) showProgress(progressChan <-chan int, operationName string) {
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()

	progress := 0
	spinner := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	spinnerIdx := 0
	stages := []string{"Starting", "Preparing", "Downloading", "Installing", "Configuring", "Finalizing"}
	stageIdx := 0

	// Clear the line and move cursor to beginning
	clearLine := func() {
		fmt.Print("\r\033[K") // ANSI escape code to clear line
	}

	for {
		select {
		case p, ok := <-progressChan:
			if !ok {
				return
			}
			progress = p

			// Update stage based on progress
			if progress < 20 {
				stageIdx = 0
			} else if progress < 40 {
				stageIdx = 1
			} else if progress < 60 {
				stageIdx = 2
			} else if progress < 80 {
				stageIdx = 3
			} else if progress < 95 {
				stageIdx = 4
			} else {
				stageIdx = 5
			}

			if progress >= 100 {
				clearLine()
				fmt.Print("\n") // Add newline before success message
				// Use consistent success format without emoji
				i.logger.Success("%s completed", operationName)
				return
			}
		case <-ticker.C:
			if progress < 100 {
				clearLine()
				currentStage := stages[stageIdx]
				fmt.Printf("\r● %s: %s %s", operationName, currentStage, spinner[spinnerIdx])
				spinnerIdx = (spinnerIdx + 1) % len(spinner)

				// Simulate progress if actual progress is not being reported
				if progress < 95 {
					progress += 2
				}
			}
		}
	}
}

// installBinary copies the current executable to the system binary path for updates and cron jobs
func (i *Installer) installBinary() error {
	if os.Getenv("ENV") == "test" {
		i.logger.InfoWithTime("Skipping binary installation in test environment")
		return nil
	}

	// Get the current executable path
	currentExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get current executable path: %w", err)
	}

	i.logger.InfoWithTime("Installing binary from %s to %s", currentExe, i.binaryPath)

	// Read the current executable
	sourceData, err := os.ReadFile(currentExe)
	if err != nil {
		return fmt.Errorf("failed to read source binary: %w", err)
	}

	// Write to destination
	if err := os.WriteFile(i.binaryPath, sourceData, 0755); err != nil {
		return fmt.Errorf("failed to write binary to %s: %w", i.binaryPath, err)
	}

	i.logger.Success("Binary installed successfully at %s", i.binaryPath)
	return nil
}
