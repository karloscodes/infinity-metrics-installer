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
	"infinity-metrics-installer/internal/docker"
	"infinity-metrics-installer/internal/logging"
)

// Installer handles the installation and update process
type Installer struct {
	logger *logging.Logger
	config *config.Config
	docker *docker.Docker
}

// NewInstaller creates a new installer instance
func NewInstaller(logger *logging.Logger) *Installer {
	return &Installer{
		logger: logger,
		config: config.NewConfig(logger),
		docker: docker.NewDocker(logger),
	}
}

// GetConfig returns the installer's configuration
func (i *Installer) GetConfig() *config.Config {
	return i.config
}

// Run executes the installation process
func (i *Installer) Run() error {
	totalSteps := 4

	i.logger.Step(1, totalSteps, "Checking system privileges")
	if os.Geteuid() != 0 {
		return fmt.Errorf("this installer must be run as root")
	}

	i.logger.Step(2, totalSteps, "Setting up Docker")
	if err := i.docker.EnsureInstalled(); err != nil {
		return fmt.Errorf("failed to install Docker: %w", err)
	}

	i.logger.Step(3, totalSteps, "Configuring Infinity Metrics")
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

	i.logger.Step(4, totalSteps, "Deploying Infinity Metrics")
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
	downloadURL := fmt.Sprintf("%s-%s", url, arch) // e.g., "https://.../infinity-metrics-v1.0.1-amd64"
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
