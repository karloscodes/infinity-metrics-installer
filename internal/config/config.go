package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"infinity-metrics-installer/internal/logging"
)

// ConfigData holds the typed configuration values
type ConfigData struct {
	// Required settings
	Domain     string
	AdminEmail string
	LicenseKey string

	// Docker settings
	DockerRegistry string
	DockerImage    string
	DockerImageTag string

	// System settings
	InstallDir string

	// Git settings
	DeploymentRepoURL string
}

// Config manages application configuration
type Config struct {
	logger *logging.Logger
	values map[string]string
	data   ConfigData
}

// Option is a functional option for configuring Config
type Option func(*Config)

// WithLogger sets the logger for Config operations
func WithLogger(logger *logging.Logger) Option {
	return func(c *Config) {
		c.logger = logger
	}
}

// WithInstallDir sets the installation directory
func WithInstallDir(dir string) Option {
	return func(c *Config) {
		c.data.InstallDir = dir
	}
}

// NewConfig creates a new Config
func NewConfig(options ...Option) *Config {
	// Create with sane defaults
	c := &Config{
		logger: logging.NewLogger(logging.Config{Level: "info"}),
		values: make(map[string]string),
		data: ConfigData{
			DockerRegistry:    "karloscodes",
			DockerImage:       "infinity-metrics-beta",
			DockerImageTag:    "latest",
			InstallDir:        "/opt/infinity-metrics",
			DeploymentRepoURL: "https://github.com/karloscodes/infinity-metrics-deployment.git",
		},
	}

	// Apply options
	for _, option := range options {
		option(c)
	}

	// Initialize values map from defaults
	c.syncDataToValues()

	return c
}

// syncDataToValues updates the values map from the structured data
func (c *Config) syncDataToValues() {
	c.values["DOMAIN"] = c.data.Domain
	c.values["ADMIN_EMAIL"] = c.data.AdminEmail
	c.values["INFINITY_METRICS_LICENSE_KEY"] = c.data.LicenseKey
	c.values["DOCKER_REGISTRY"] = c.data.DockerRegistry
	c.values["TAG"] = c.data.DockerImageTag
}

// syncValuesToData updates the structured data from the values map
func (c *Config) syncValuesToData() {
	c.data.Domain = c.values["DOMAIN"]
	c.data.AdminEmail = c.values["ADMIN_EMAIL"]
	c.data.LicenseKey = c.values["INFINITY_METRICS_LICENSE_KEY"]
	c.data.DockerRegistry = c.GetString("DOCKER_REGISTRY", "localhost")
	c.data.DockerImageTag = c.GetString("TAG", "latest")
}

// CollectFromUser collects configuration from user input
func (c *Config) CollectFromUser() error {
	reader := bufio.NewReader(os.Stdin)

	// Domain name
	c.logger.Info("Enter your domain name (e.g., analytics.example.com):")
	domainName, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read domain name: %w", err)
	}
	domain := strings.TrimSpace(domainName)
	if domain == "" {
		return fmt.Errorf("domain name cannot be empty")
	}
	c.data.Domain = domain

	// Admin email
	c.logger.Info("Enter admin email address (for SSL certificates):")
	adminEmail, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read admin email: %w", err)
	}
	email := strings.TrimSpace(adminEmail)
	if email == "" {
		return fmt.Errorf("admin email cannot be empty")
	}
	c.data.AdminEmail = email

	// License key
	c.logger.Info("Enter your Infinity Metrics license key:")
	licenseKey, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read license key: %w", err)
	}
	license := strings.TrimSpace(licenseKey)
	if license == "" {
		return fmt.Errorf("license key cannot be empty")
	}
	c.data.LicenseKey = license

	// Synchronize the data to the values map
	c.syncDataToValues()

	c.logger.Success("Configuration collected successfully")
	return nil
}

// LoadFromFile loads configuration from a .env file
func (c *Config) LoadFromFile(filename string) error {
	c.logger.Info("Loading configuration from %s", filename)

	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open config file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNumber := 0

	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		line = strings.TrimSpace(line)

		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse key=value
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			c.logger.Warn("Invalid configuration line %d: %s", lineNumber, line)
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		// Remove quotes if present
		if len(value) >= 2 && (value[0] == '"' && value[len(value)-1] == '"') ||
			(value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		}

		c.values[key] = value
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading config file: %w", err)
	}

	// Update the structured data from the values map
	c.syncValuesToData()

	c.logger.Success("Configuration loaded successfully")
	return nil
}

// SaveToFile saves the configuration to a .env file
func (c *Config) SaveToFile(filename string) error {
	c.logger.Info("Saving configuration to %s", filename)

	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	writer.WriteString("# Infinity Metrics Configuration\n")
	writer.WriteString(fmt.Sprintf("# Generated on %s\n\n", time.Now().Format(time.RFC3339)))

	// Write all configuration values
	for key, value := range c.values {
		_, err := writer.WriteString(fmt.Sprintf("%s=%s\n", key, value))
		if err != nil {
			return fmt.Errorf("failed to write to config file: %w", err)
		}
	}

	if err := writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush config file: %w", err)
	}

	c.logger.Success("Configuration saved successfully")
	return nil
}

// GetString gets a string value from the config with a default
func (c *Config) GetString(key, defaultValue string) string {
	if val, ok := c.values[key]; ok && val != "" {
		return val
	}
	return defaultValue
}

// SetString sets a string value in the config
func (c *Config) SetString(key, value string) {
	c.values[key] = value
	c.syncValuesToData() // Keep data in sync
}

// GetBool gets a boolean value from the config
func (c *Config) GetBool(key string, defaultValue bool) bool {
	if val, ok := c.values[key]; ok {
		lowerVal := strings.ToLower(val)
		return lowerVal == "true" || lowerVal == "1" || lowerVal == "yes" || lowerVal == "y"
	}
	return defaultValue
}

// SetBool sets a boolean value in the config
func (c *Config) SetBool(key string, value bool) {
	if value {
		c.values[key] = "true"
	} else {
		c.values[key] = "false"
	}
	c.syncValuesToData() // Keep data in sync
}

// GetData returns a copy of the typed configuration
func (c *Config) GetData() ConfigData {
	return c.data
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	// Required fields
	if c.data.Domain == "" {
		return fmt.Errorf("domain name is required")
	}

	if c.data.AdminEmail == "" {
		return fmt.Errorf("admin email is required")
	}

	if c.data.LicenseKey == "" {
		return fmt.Errorf("license key is required")
	}

	return nil
}
