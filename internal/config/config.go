package config

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"infinity-metrics-installer/internal/logging"
)

// ConfigData holds the configuration
type ConfigData struct {
	Domain           string // Local: User-provided
	AdminEmail       string // Local: User-provided
	LicenseKey       string // Local: User-provided
	AppImage         string // Server/Default: e.g., "karloscodes/infinity-metrics-beta:latest"
	CaddyImage       string // Server/Default: e.g., "caddy:2.7-alpine"
	InstallDir       string // Default: e.g., "/opt/infinity-metrics"
	BackupPath       string // Default: SQLite backup location
	ConfigVersion    string // Local/Server: Tracks applied config version
	InstallerVersion string // Server: Version of the infinity-metrics binary
	InstallerURL     string // Server: Base URL to download new infinity-metrics binary
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
			AppImage:         "karloscodes/infinity-metrics-beta:latest",
			CaddyImage:       "caddy:2.7-alpine",
			InstallDir:       "/opt/infinity-metrics",
			BackupPath:       "/opt/infinity-metrics/storage/backups",
			ConfigVersion:    "0.0.0",
			InstallerVersion: "0.0.0", // Initial version
			InstallerURL:     "",      // No default URL
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
			return fmt.Errorf("input cannot be empty")
		}
	}
	c.logger.Success("Configuration collected")
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
		case "CONFIG_VERSION":
			c.data.ConfigVersion = value
		case "INSTALLER_VERSION":
			c.data.InstallerVersion = value
		case "INSTALLER_URL":
			c.data.InstallerURL = value
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}
	c.logger.Success("Configuration loaded")
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
	fmt.Fprintf(file, "CONFIG_VERSION=%s\n", c.data.ConfigVersion)
	fmt.Fprintf(file, "INSTALLER_VERSION=%s\n", c.data.InstallerVersion)
	fmt.Fprintf(file, "INSTALLER_URL=%s\n", c.data.InstallerURL)

	c.logger.Success("Configuration saved")
	return nil
}

// FetchFromServer updates from config server
func (c *Config) FetchFromServer(url string) error {
	c.logger.Info("Fetching from %s", url)
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to fetch config: %w", err)
	}
	defer resp.Body.Close()

	var serverData struct {
		AppImage         string `json:"app_image"`
		CaddyImage       string `json:"caddy_image"`
		ConfigVersion    string `json:"config_version"`
		InstallerVersion string `json:"installer_version"`
		InstallerURL     string `json:"installer_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&serverData); err != nil {
		return fmt.Errorf("failed to decode config: %w", err)
	}

	if serverData.ConfigVersion == "" {
		return fmt.Errorf("config_version missing from server config")
	}
	// No version check here—always update binary in Update()

	if serverData.AppImage != "" {
		c.data.AppImage = serverData.AppImage
	}
	if serverData.CaddyImage != "" {
		c.data.CaddyImage = serverData.CaddyImage
	}
	if serverData.InstallerVersion != "" {
		c.data.InstallerVersion = serverData.InstallerVersion
	}
	if serverData.InstallerURL != "" {
		c.data.InstallerURL = serverData.InstallerURL
	}
	c.data.ConfigVersion = serverData.ConfigVersion

	c.logger.Success("Server config fetched, version %s", serverData.ConfigVersion)
	return nil
}

// GetData returns the config data
func (c *Config) GetData() ConfigData {
	return c.data
}

// Validate checks required fields
func (c *Config) Validate() error {
	if c.data.Domain == "" || c.data.AdminEmail == "" || c.data.LicenseKey == "" {
		return fmt.Errorf("domain, admin email, and license key are required")
	}
	return nil
}
