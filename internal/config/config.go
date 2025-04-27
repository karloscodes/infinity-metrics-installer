package config

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"

	"golang.org/x/term"

	"infinity-metrics-installer/internal/logging"
)

// GithubRepo is the centralized GitHub repository URL slug
const GithubRepo = "karloscodes/infinity-metrics-installer"

// ConfigData holds the configuration
type ConfigData struct {
	Domain        string // Local: User-provided
	AdminEmail    string // Local: User-provided
	LicenseKey    string // Local: User-provided
	AdminPassword string // Local: User-provided, held in memory only
	AppImage      string // GitHub Release/Default: e.g., "karloscodes/infinity-metrics-beta:latest"
	CaddyImage    string // GitHub Release/Default: e.g., "caddy:2.7-alpine"
	InstallDir    string // Default: e.g., "/opt/infinity-metrics"
	BackupPath    string // Default: SQLite backup location
	PrivateKey    string // Generated: secure random key for INFINITY_METRICS_PRIVATE_KEY
	Version       string // GitHub Release: Version of the infinity-metrics binary (optional)
	InstallerURL  string // GitHub Release: URL to download new infinity-metrics binary
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
			Domain:        "", // Required from user
			AdminEmail:    "", // Required from user
			AdminPassword: "", // Required from user, held in memory
			LicenseKey:    "", // Required from user
			AppImage:      "karloscodes/infinity-metrics-beta:latest",
			CaddyImage:    "caddy:2.7-alpine",
			InstallDir:    "/opt/infinity-metrics",
			BackupPath:    "/opt/infinity-metrics/storage/backups",
			PrivateKey:    "",
			Version:       "latest",
			InstallerURL:  fmt.Sprintf("https://github.com/%s/releases/latest", GithubRepo),
		},
	}
}

// CollectFromUser gets required user input upfront
func (c *Config) CollectFromUser(reader *bufio.Reader) error {
	for {
		c.data.Domain = ""
		c.data.AdminEmail = ""
		c.data.LicenseKey = ""
		c.data.AdminPassword = ""
		c.data.InstallDir = "/opt/infinity-metrics"

		fmt.Print("Enter your domain name (e.g., analytics.example.com). A/AAAA records must be set at this point to autoconfigure SSL: ")
		domain, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read domain: %w", err)
		}
		c.data.Domain = strings.TrimSpace(domain)
		if c.data.Domain == "" {
			fmt.Println("Error: Domain cannot be empty.")
			continue
		}

		ips, err := net.LookupIP(c.data.Domain)
		if err != nil {
			fmt.Printf("Error: Invalid domain: %v\n", err)
			continue
		}
		if len(ips) == 0 {
			fmt.Println("Error: No A/AAAA records found for the provided domain. Please set up the DNS records and try again.")
			fmt.Println("Note:")
			fmt.Println("DNS propagation may take from a few minutes to hours to complete.")
			fmt.Println("You can check the DNS records at https://mxtoolbox.com/SuperTool.aspx or https://dnschecker.org/")
			continue
		}

		fmt.Print("Enter admin email address: ")
		adminEmail, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read admin email: %w", err)
		}
		c.data.AdminEmail = strings.TrimSpace(adminEmail)
		if c.data.AdminEmail == "" {
			fmt.Println("Error: Admin email cannot be empty.")
			continue
		}

		emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
		if !emailRegex.MatchString(c.data.AdminEmail) {
			fmt.Println("Error: Invalid email address. Please enter a valid email (e.g., user@example.com).")
			continue
		}

		fmt.Print("Enter your Infinity Metrics license key: ")
		licenseKey, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read license key: %w", err)
		}
		c.data.LicenseKey = strings.TrimSpace(licenseKey)
		if c.data.LicenseKey == "" {
			fmt.Println("Error: License key cannot be empty.")
			continue
		}

		adminPassword := os.Getenv("ADMIN_PASSWORD")
		if adminPassword != "" {
			c.data.AdminPassword = adminPassword
			c.logger.Info("Using admin password from environment variable")
		} else {
			for {
				fmt.Print("Enter admin password (minimum 8 characters): ")
				passwordBytes, err := term.ReadPassword(int(syscall.Stdin))
				if err != nil {
					return fmt.Errorf("failed to read password: %w", err)
				}
				fmt.Println()

				c.data.AdminPassword = strings.TrimSpace(string(passwordBytes))
				if c.data.AdminPassword == "" {
					fmt.Println("Error: Password cannot be empty.")
					continue
				}
				if len(c.data.AdminPassword) < 8 {
					fmt.Println("Error: Password must be at least 8 characters long.")
					continue
				}

				fmt.Print("Confirm admin password: ")
				confirmPasswordBytes, err := term.ReadPassword(int(syscall.Stdin))
				if err != nil {
					return fmt.Errorf("failed to read confirmation password: %w", err)
				}
				fmt.Println()

				confirmPassword := strings.TrimSpace(string(confirmPasswordBytes))
				if c.data.AdminPassword != confirmPassword {
					fmt.Println("Error: Passwords do not match. Please try again.")
					continue
				}
				break
			}
		}

		c.data.BackupPath = filepath.Join(c.data.InstallDir, "storage", "backups")

		fmt.Println("\nConfiguration Summary:")
		fmt.Printf("Domain: %s\n", c.data.Domain)
		fmt.Printf("%s => %s\n", c.data.Domain, ips)
		fmt.Printf("Admin Email: %s\n", c.data.AdminEmail)
		fmt.Printf("License Key: %s\n", c.data.LicenseKey)
		fmt.Printf("Installation Directory: %s\n", c.data.InstallDir)
		fmt.Printf("Backup Path: %s\n", c.data.BackupPath)

		fmt.Print("\nProceed with this configuration? [Y/n]: ")
		confirmStr, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read confirmation: %w", err)
		}

		confirmStr = strings.TrimSpace(strings.ToLower(confirmStr))
		if confirmStr == "y" || confirmStr == "yes" || confirmStr == "" {
			break
		}

		fmt.Println("Configuration declined. Let's start over.")
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
		case "INFINITY_METRICS_DOMAIN":
			c.data.Domain = value
		case "INFINITY_METRICS_ADMIN_EMAIL":
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
		case "INFINITY_METRICS_PRIVATE_KEY":
			c.data.PrivateKey = value
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// If PrivateKey is missing, generate one and append to file
	if c.data.PrivateKey == "" {
		pk, err := generatePrivateKey()
		if err != nil {
			return err
		}
		c.data.PrivateKey = pk
		// Append to file
		f, ferr := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY, 0)
		if ferr == nil {
			fmt.Fprintf(f, "INFINITY_METRICS_PRIVATE_KEY=%s\n", pk)
			f.Close()
			c.logger.Info("Added missing INFINITY_METRICS_PRIVATE_KEY to %s", filename)
		}
	}
	c.logger.Success("Configuration loaded from %s", filename)
	return nil
}

// SaveToFile saves local config to .env
func (c *Config) SaveToFile(filename string) error {
	c.logger.Info("Saving to %s", filename)

	// Ensure private key is set
	if c.data.PrivateKey == "" {
		pk, err := generatePrivateKey()
		if err != nil {
			return err
		}
		c.data.PrivateKey = pk
		c.logger.Info("Generated new INFINITY_METRICS_PRIVATE_KEY")
	}

	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	fmt.Fprintf(file, "INFINITY_METRICS_DOMAIN=%s\n", c.data.Domain)
	fmt.Fprintf(file, "INFINITY_METRICS_ADMIN_EMAIL=%s\n", c.data.AdminEmail)
	fmt.Fprintf(file, "INFINITY_METRICS_LICENSE_KEY=%s\n", c.data.LicenseKey)
	fmt.Fprintf(file, "APP_IMAGE=%s\n", c.data.AppImage)
	fmt.Fprintf(file, "CADDY_IMAGE=%s\n", c.data.CaddyImage)
	fmt.Fprintf(file, "INSTALL_DIR=%s\n", c.data.InstallDir)
	fmt.Fprintf(file, "BACKUP_PATH=%s\n", c.data.BackupPath)
	fmt.Fprintf(file, "VERSION=%s\n", c.data.Version)
	fmt.Fprintf(file, "INSTALLER_URL=%s\n", c.data.InstallerURL)
	fmt.Fprintf(file, "INFINITY_METRICS_PRIVATE_KEY=%s\n", c.data.PrivateKey)

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
		TagName string `json:"tag_name"`
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

	version := strings.TrimPrefix(release.TagName, "v")
	if version == "" {
		c.logger.Warn("No valid version found in release tag: %s", release.TagName)
		c.logger.Info("Falling back to hardcoded default configuration")
		return nil
	}

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

	if configURL != "" {
		if err := c.fetchConfigJSON(configURL); err != nil {
			c.logger.Warn("Failed to fetch config.json from %s: %v", configURL, err)
		}
	} else {
		c.logger.Warn("config.json not found in latest release assets")
	}

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

// SetCaddyImage sets the CaddyImage field in ConfigData
func (c *Config) SetCaddyImage(image string) {
	c.data.CaddyImage = image
	c.logger.Info("CaddyImage updated to: %s", image)
}

// SetInstallDir sets the InstallDir field in ConfigData
func (c *Config) SetInstallDir(dir string) {
	c.data.InstallDir = dir
}

// SetInstallerURL sets the InstallerURL field in ConfigData
func (c *Config) SetInstallerURL(url string) {
	c.data.InstallerURL = url
}

// GetMainDBPath returns the main database path
func (c *Config) GetMainDBPath() string {
	return filepath.Join(c.data.InstallDir, "storage", "infinity-metrics-production.db")
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
	if c.data.AdminPassword == "" {
		return fmt.Errorf("password is required")
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
	if c.data.PrivateKey == "" {
		return fmt.Errorf("private key is required")
	}
	return nil
}

// generatePrivateKey generates a secure random private key
func generatePrivateKey() (string, error) {
	key := make([]byte, 16)
	_, err := rand.Read(key)
	if err != nil {
		return "", fmt.Errorf("failed to generate private key: %w", err)
	}
	return hex.EncodeToString(key), nil
}
