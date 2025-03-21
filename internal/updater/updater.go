package updater

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"infinity-metrics-installer/internal/config"
	"infinity-metrics-installer/internal/database"
	"infinity-metrics-installer/internal/docker"
	"infinity-metrics-installer/internal/logging"
)

const (
	GitHubRepo        = "karloscodes/infinity-metrics-installer"
	GitHubAPIURL      = "https://api.github.com/repos/" + GitHubRepo + "/releases/latest"
	BinaryInstallPath = "/usr/local/bin/infinity-metrics" // Standard installation path
)

type Updater struct {
	logger   *logging.Logger
	config   *config.Config
	docker   *docker.Docker
	database *database.Database
}

func NewUpdater(logger *logging.Logger) *Updater {
	// Create a new file logger for the Updater with a specific log file name
	fileLogger := logging.NewFileLogger(logging.Config{
		Level:   logger.Level.String(), // Match the log level from the main logger
		Verbose: logger.GetVerbose(),   // Use getter method
		Quiet:   logger.GetQuiet(),     // Use getter method
		LogDir:  "",                    // Use default log directory (/opt/infinity-metrics/logs)
		LogFile: "updater.log",         // Specify the log file name for the Updater
	})

	db := database.NewDatabase(fileLogger)
	return &Updater{
		logger:   fileLogger, // Use the file logger for the Updater
		config:   config.NewConfig(fileLogger),
		docker:   docker.NewDocker(fileLogger, db),
		database: db,
	}
}

func (u *Updater) Run(currentVersion string) error {
	data := u.config.GetData()
	envFile := filepath.Join(data.InstallDir, ".env")

	u.logger.Info("Loading configuration")
	if err := u.config.LoadFromFile(envFile); err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	u.logger.Info("Checking for updates from server")
	if err := u.config.FetchFromServer(""); err != nil {
		u.logger.Warn("Server config fetch failed, using local: %v", err)
	}

	// Fetch the latest version from GitHub
	latestVersion, binaryURL, err := u.getLatestVersionAndBinaryURL()
	if err != nil {
		u.logger.Warn("Failed to fetch latest version from GitHub: %v", err)
		// Fall back to using the version from the config
		latestVersion = extractVersionFromURL(u.config.GetData().InstallerURL)
		if latestVersion == "" {
			u.logger.Warn("Could not determine latest version from URL: %s", u.config.GetData().InstallerURL)
		}
	}

	// Compare versions and update binary if necessary
	if latestVersion != "" {
		if compareVersions(currentVersion, latestVersion) < 0 {
			u.logger.Info("Local version %s is older than latest %s, updating binary...", currentVersion, latestVersion)
			arch := runtime.GOARCH
			if arch != "amd64" && arch != "arm64" {
				return fmt.Errorf("unsupported architecture: %s", arch)
			}

			// Use the binary URL from GitHub if available, otherwise fall back to InstallerURL
			downloadURL := binaryURL
			if downloadURL == "" {
				downloadURL = u.config.GetData().InstallerURL
				if downloadURL == "" || downloadURL == fmt.Sprintf("https://github.com/%s/releases/latest", config.GithubRepo) {
					downloadURL = fmt.Sprintf("https://github.com/%s/releases/download/v%s/infinity-metrics-v%s-%s", GitHubRepo, latestVersion, latestVersion, arch)
				}
			}

			if err := u.updateBinary(downloadURL, BinaryInstallPath); err != nil {
				u.logger.Warn("Failed to update binary: %v", err)
			} else {
				u.logger.Success("Binary updated to version %s, restarting", latestVersion)
				return exec.Command(BinaryInstallPath, "update").Run()
			}
		} else {
			u.logger.Info("Current version %s matches or is newer than latest %s, no binary update needed", currentVersion, latestVersion)
		}
	}

	if err := u.update(); err != nil {
		return fmt.Errorf("update failed: %w", err)
	}
	if err := u.config.SaveToFile(envFile); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	u.logger.Success("Update completed")
	return nil
}

func (u *Updater) getLatestVersionAndBinaryURL() (string, string, error) {
	u.logger.Info("Fetching latest release from GitHub: %s", GitHubAPIURL)
	resp, err := http.Get(GitHubAPIURL)
	if err != nil {
		return "", "", fmt.Errorf("failed to fetch latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("failed to fetch latest release, status: %s", resp.Status)
	}

	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name       string `json:"name"`
			BrowserURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", "", fmt.Errorf("failed to parse release JSON: %w", err)
	}

	// Extract the version from the tag name (e.g., "v1.0.3" -> "1.0.3")
	latestVersion := strings.TrimPrefix(release.TagName, "v")
	if latestVersion == "" {
		return "", "", fmt.Errorf("invalid version in release tag: %s", release.TagName)
	}

	// Find the binary for the current architecture
	arch := runtime.GOARCH
	expectedAsset := fmt.Sprintf("infinity-metrics-v%s-%s", latestVersion, arch)
	var binaryURL string
	for _, asset := range release.Assets {
		if asset.Name == expectedAsset {
			binaryURL = asset.BrowserURL
			break
		}
	}

	if binaryURL == "" {
		return latestVersion, "", fmt.Errorf("no binary found for architecture %s in release v%s", arch, latestVersion)
	}

	return latestVersion, binaryURL, nil
}

func (u *Updater) update() error {
	totalSteps := 3

	u.logger.Info("Step 1/%d: Loading configuration", totalSteps)
	data := u.config.GetData()
	envFile := filepath.Join(data.InstallDir, ".env")
	if err := u.config.LoadFromFile(envFile); err != nil {
		return fmt.Errorf("failed to load config from %s: %w", envFile, err)
	}

	u.logger.Info("Step 2/%d: Checking for updates from server", totalSteps)
	if err := u.config.FetchFromServer(""); err != nil {
		u.logger.Warn("Server config fetch failed, using local config: %v", err)
	}

	u.logger.Info("Step 3/%d: Applying updates", totalSteps)
	mainDBPath := u.config.GetMainDBPath()
	backupDir := u.config.GetData().BackupPath
	if _, err := u.database.BackupDatabase(mainDBPath, backupDir); err != nil {
		u.logger.Warn("Failed to backup database before update: %v", err)
		u.logger.Warn("Proceeding with update without backup")
	} else {
		u.logger.Success("Database backup created successfully")
	}

	if err := u.docker.Update(u.config); err != nil {
		return fmt.Errorf("failed to update Docker containers: %w", err)
	}

	if err := u.config.SaveToFile(envFile); err != nil {
		return fmt.Errorf("failed to save config to %s: %w", envFile, err)
	}

	u.logger.Success("Update completed successfully")
	return nil
}

func (u *Updater) updateBinary(url, binaryPath string) error {
	u.logger.InfoWithTime("Downloading new installer binary from %s", url)
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed, status: %s", resp.Status)
	}

	// Use a temporary file in /tmp to avoid permission issues
	newBinary := filepath.Join("/tmp", "infinity-metrics.new")
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

	// Move the new binary to the final location
	if err := os.Rename(newBinary, binaryPath); err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}

	u.logger.Success("Binary updated successfully")
	return nil
}

func compareVersions(v1, v2 string) int {
	// Split version strings into parts
	v1Parts := strings.Split(v1, ".")
	v2Parts := strings.Split(v2, ".")

	// Ensure both versions have the same number of parts
	maxParts := len(v1Parts)
	if len(v2Parts) > maxParts {
		maxParts = len(v2Parts)
	}
	for i := len(v1Parts); i < maxParts; i++ {
		v1Parts = append(v1Parts, "0")
	}
	for i := len(v2Parts); i < maxParts; i++ {
		v2Parts = append(v2Parts, "0")
	}

	// Compare each part
	for i := 0; i < maxParts; i++ {
		v1Num := 0
		v2Num := 0
		fmt.Sscanf(v1Parts[i], "%d", &v1Num)
		fmt.Sscanf(v2Parts[i], "%d", &v2Num)

		if v1Num < v2Num {
			return -1
		} else if v1Num > v2Num {
			return 1
		}
	}
	return 0
}

func extractVersionFromURL(url string) string {
	parts := strings.Split(url, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, "infinity-metrics-v") && i < len(parts) {
			filename := part
			if strings.HasPrefix(filename, "infinity-metrics-v") {
				version := strings.TrimPrefix(filename, "infinity-metrics-v")
				version = strings.TrimSuffix(version, "-amd64")
				version = strings.TrimSuffix(version, "-arm64")
				return version
			}
		}
	}
	return ""
}
