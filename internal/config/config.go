package config

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"

	"infinity-metrics-installer/internal/logging"
)

// GithubRepo is the centralized GitHub repository URL slug
const GithubRepo = "karloscodes/infinity-metrics-installer"

// ConfigData holds the configuration
type ConfigData struct {
	Domain       string // Local: User-provided
	AdminEmail   string // Local: User-provided
	LicenseKey   string // Local: User-provided
	AppImage     string // GitHub Release/Default: e.g., "karloscodes/infinity-metrics-beta:latest"
	CaddyImage   string // GitHub Release/Default: e.g., "caddy:2.7-alpine"
	InstallDir   string // Default: e.g., "/opt/infinity-metrics"
	BackupPath   string // Default: SQLite backup location
	Version      string // GitHub Release: Version of the infinity-metrics binary
	InstallerURL string // GitHub Release: URL to download new infinity-metrics binary
}

// Config manages configuration
type Config struct {
	logger *logging.Logger
	data   ConfigData
}

// NewConfig creates a Config with defaults
func NewConfig(logger *logging.Logger) *Config {
	return &Config{
		logger: logger,
		data: ConfigData{
			Domain:       "", // Required from user
			AdminEmail:   "", // Required from user
			LicenseKey:   "", // Required from user
			AppImage:     "karloscodes/infinity-metrics-beta:latest",
			CaddyImage:   "caddy:2.7-alpine",
			InstallDir:   "/opt/infinity-metrics",
			BackupPath:   "/opt/infinity-metrics/storage/backups",
			Version:      "0.0.0",
			InstallerURL: fmt.Sprintf("https://github.com/%s/releases/latest", GithubRepo), // Default base URL
		},
	}
}

// CollectFromUser gets required user input
func (c *Config) CollectFromUser() error {
	reader := bufio.NewReader(os.Stdin)
	for _, p := range []struct {
		prompt string
		field  *string
	}{
		{"Enter your domain name (e.g., analytics.example.com):", &c.data.Domain},
		{"Enter admin email address (for SSL certificates):", &c.data.AdminEmail},
		{"Enter your Infinity Metrics license key:", &c.data.LicenseKey},
	} {
		c.logger.Info(p.prompt)
		input, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read input: %w", err)
		}
		*p.field = strings.TrimSpace(input)
		if *p.field == "" {
			return fmt.Errorf("input for %s cannot be empty", p.prompt)
		}
	}
	c.logger.Success("Configuration collected from user")
	return nil
}

// LoadFromFile loads local config from .env
func (c *Config) LoadFromFile(filename string) error {
	c.logger.Info("Loading from %s", filename)
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key, value := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		switch key {
		case "DOMAIN":
			c.data.Domain = value
		case "ADMIN_EMAIL":
			c.data.AdminEmail = value
		case "INFINITY_METRICS_LICENSE_KEY":
			c.data.LicenseKey = value
		case "APP_IMAGE":
			c.data.AppImage = value
		case "CADDY_IMAGE":
			c.data.CaddyImage = value
		case "INSTALL_DIR":
			c.data.InstallDir = value
		case "BACKUP_PATH":
			c.data.BackupPath = value
		case "VERSION":
			c.data.Version = value
		case "INSTALLER_URL":
			c.data.InstallerURL = value
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}
	c.logger.Success("Configuration loaded from %s", filename)
	return nil
}

// SaveToFile saves local config to .env
func (c *Config) SaveToFile(filename string) error {
	c.logger.Info("Saving to %s", filename)
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	fmt.Fprintf(file, "DOMAIN=%s\n", c.data.Domain)
	fmt.Fprintf(file, "ADMIN_EMAIL=%s\n", c.data.AdminEmail)
	fmt.Fprintf(file, "INFINITY_METRICS_LICENSE_KEY=%s\n", c.data.LicenseKey)
	fmt.Fprintf(file, "APP_IMAGE=%s\n", c.data.AppImage)
	fmt.Fprintf(file, "CADDY_IMAGE=%s\n", c.data.CaddyImage)
	fmt.Fprintf(file, "INSTALL_DIR=%s\n", c.data.InstallDir)
	fmt.Fprintf(file, "BACKUP_PATH=%s\n", c.data.BackupPath)
	fmt.Fprintf(file, "VERSION=%s\n", c.data.Version)

	c.logger.Success("Configuration saved to %s", filename)
	return nil
}

// FetchFromServer fetches config from the latest GitHub release
func (c *Config) FetchFromServer(_ string) error {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", GithubRepo)
	c.logger.Info("Fetching latest release from GitHub: %s", url)

	resp, err := http.Get(url)
	if err != nil || resp.StatusCode != http.StatusOK {
		c.logger.Warn("Failed to fetch latest release: %v", err)
		if resp != nil {
			c.logger.Warn("GitHub API returned status: %s", resp.Status)
		}
		c.logger.Info("Falling back to hardcoded default configuration")
		return nil
	}
	defer resp.Body.Close()

	var release struct {
		TagName string `json:"tag_name"` // e.g., "v1.0.0"
		Assets  []struct {
			Name        string `json:"name"`
			DownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		c.logger.Warn("Failed to decode GitHub release data: %v", err)
		c.logger.Info("Falling back to hardcoded default configuration")
		return nil
	}

	// Extract version from tag_name (e.g., "v1.0.0" -> "1.0.0")
	version := strings.TrimPrefix(release.TagName, "v")
	if version == "" {
		c.logger.Warn("No valid version found in release tag: %s", release.TagName)
		c.logger.Info("Falling back to hardcoded default configuration")
		return nil
	}

	// Find config.json and binary URLs
	var configURL string
	binaryName := fmt.Sprintf("infinity-metrics-v%s-%s", version, runtime.GOARCH)
	var binaryURL string

	for _, asset := range release.Assets {
		switch asset.Name {
		case "config.json":
			configURL = asset.DownloadURL
		case binaryName:
			binaryURL = asset.DownloadURL
		}
	}

	// Fetch config.json if available
	if configURL != "" {
		if err := c.fetchConfigJSON(configURL); err != nil {
			c.logger.Warn("Failed to fetch config.json from %s: %v", configURL, err)
		}
	} else {
		c.logger.Warn("config.json not found in latest release assets")
	}

	// Update fields from release
	c.data.Version = version
	if binaryURL != "" {
		c.data.InstallerURL = binaryURL
	} else {
		c.logger.Warn("Binary %s not found in latest release, keeping default URL", binaryName)
	}

	c.logger.Success("Fetched configuration from GitHub release %s", release.TagName)
	return nil
}

// fetchConfigJSON fetches and applies config.json from a URL
func (c *Config) fetchConfigJSON(url string) error {
	c.logger.Info("Fetching config.json from %s", url)
	resp, err := http.Get(url)
	if err != nil || resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch config.json: %v, status: %s", err, resp.Status)
	}
	defer resp.Body.Close()

	var serverData struct {
		AppImage   string `json:"app_image"`
		CaddyImage string `json:"caddy_image"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&serverData); err != nil {
		return fmt.Errorf("failed to decode config.json: %w", err)
	}

	// Update fields only if provided
	if serverData.AppImage != "" {
		c.data.AppImage = serverData.AppImage
	}
	if serverData.CaddyImage != "" {
		c.data.CaddyImage = serverData.CaddyImage
	}

	c.logger.Success("Applied config.json from release")
	return nil
}

// GetData returns the config data
func (c *Config) GetData() ConfigData {
	return c.data
}

func (c *Config) GetMainDBPath() string {
	return c.data.InstallDir + "/storage/infinity-metrics-production.db"
}

// Validate checks required fields
func (c *Config) Validate() error {
	if c.data.Domain == "" {
		return fmt.Errorf("domain is required")
	}
	if c.data.AdminEmail == "" {
		return fmt.Errorf("admin email is required")
	}
	if c.data.LicenseKey == "" {
		return fmt.Errorf("license key is required")
	}
	if c.data.AppImage == "" {
		return fmt.Errorf("app image is required")
	}
	if c.data.CaddyImage == "" {
		return fmt.Errorf("caddy image is required")
	}
	if c.data.InstallDir == "" {
		return fmt.Errorf("install directory is required")
	}
	if c.data.BackupPath == "" {
		return fmt.Errorf("backup path is required")
	}
	return nil
}
