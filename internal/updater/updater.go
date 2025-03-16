package updater

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

// Run executes the update process
func (u *Updater) Run(currentVersion string) error {
	data := u.config.GetData()
	envFile := filepath.Join(data.InstallDir, ".env")

	// Load existing config
	if err := u.config.LoadFromFile(envFile); err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Fetch latest config from GitHub release
	if err := u.config.FetchFromServer(""); err != nil {
		u.logger.Warn("Server config fetch failed, using local: %v", err)
	}

	// Check if update is needed
	if data.Version == currentVersion {
		u.logger.Info("Current version %s matches latest %s, no binary update needed", currentVersion, data.Version)
	} else {
		arch := runtime.GOARCH
		if arch != "amd64" && arch != "arm64" {
			return fmt.Errorf("unsupported architecture: %s", arch)
		}
		if data.InstallerURL != "" && data.InstallerURL != fmt.Sprintf("https://github.com/%s/releases/latest", config.GithubRepo) {
			if err := u.updateBinary(data.InstallerURL, data.InstallDir, arch); err != nil {
				u.logger.Warn("Failed to update binary: %v", err)
			} else {
				u.logger.Success("Binary updated to version %s, restarting", data.Version)
				// Restart with the new binary
				return exec.Command(filepath.Join(data.InstallDir, "infinity-metrics"), "update").Run()
			}
		}
	}

	// Proceed with Docker and config updates
	if err := u.update(); err != nil {
		return fmt.Errorf("update failed: %w", err)
	}
	if err := u.config.SaveToFile(envFile); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	u.logger.Success("Update completed")
	return nil
}

// update applies Docker and config updates
func (u *Updater) update() error {
	totalSteps := 4

	u.logger.Step(1, totalSteps, "Ensuring SQLite is installed")
	if err := u.database.EnsureSQLiteInstalled(); err != nil {
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
	if err := u.config.FetchFromServer(""); err != nil {
		u.logger.Warn("Server config fetch failed, using local config: %v", err)
	}

	u.logger.Step(4, totalSteps, "Applying updates")
	// Create backup before update
	mainDBPath := u.config.GetMainDBPath()
	backupDir := u.config.GetData().BackupPath
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

// updateBinary downloads and updates the installer binary from the GitHub release
func (u *Updater) updateBinary(url, installDir, arch string) error {
	u.logger.Info("Downloading new installer binary from %s", url)
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed, status: %s", resp.Status)
	}

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

	u.logger.Success("Binary updated successfully")
	return nil
}
