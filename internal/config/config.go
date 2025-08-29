package config

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"golang.org/x/term"

	"infinity-metrics-installer/internal/errors"
	"infinity-metrics-installer/internal/logging"
	"infinity-metrics-installer/internal/validation"
)

// GithubRepo is the centralized GitHub repository URL slug
const GithubRepo = "karloscodes/infinity-metrics-installer"

// ConfigData holds the configuration
type ConfigData struct {
	Domain       string   // Local: User-provided
	AppImage     string   // GitHub Release/Default: e.g., "karloscodes/infinity-metrics-beta:latest"
	CaddyImage   string   // GitHub Release/Default: e.g., "caddy:2.7-alpine"
	InstallDir   string   // Default: e.g., "/opt/infinity-metrics"
	BackupPath   string   // Default: SQLite backup location
	PrivateKey   string   // Generated: secure random key for INFINITY_METRICS_PRIVATE_KEY
	Version      string   // GitHub Release: Version of the infinity-metrics binary (optional)
	InstallerURL string   // GitHub Release: URL to download new infinity-metrics binary
	DNSWarnings  []string // DNS configuration warnings
	User         string   // Database: Admin user email from users table
	LicenseKey   string   // License key for the application
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
			AppImage:     "karloscodes/infinity-metrics-beta:latest",
			CaddyImage:   "caddy:2.7-alpine",
			InstallDir:   "/opt/infinity-metrics",
			BackupPath:   "/opt/infinity-metrics/storage/backups",
			PrivateKey:   "",
			Version:      "latest",
			InstallerURL: fmt.Sprintf("https://github.com/%s/releases/latest", GithubRepo),
		},
	}
}

// Helper function to get the current server's primary public IP address
func getCurrentServerIP() (string, error) {
	// Try to get IPs from multiple external services for better reliability
	externalServices := []string{
		"https://api.ipify.org",
		"https://ifconfig.me/ip",
		"https://icanhazip.com",
	}

	var publicIPs []string

	// Try external services first
	for _, service := range externalServices {
		resp, err := http.Get(service)
		if err == nil {
			defer resp.Body.Close()
			ip, err := io.ReadAll(resp.Body)
			if err == nil && len(ip) > 0 {
				publicIP := strings.TrimSpace(string(ip))
				publicIPs = append(publicIPs, publicIP)
				break // We got a valid IP, no need to try other services
			}
		}
	}

	// Also collect all local interface IPs
	var localIPs []string

	ifaces, err := net.Interfaces()
	if err != nil {
		// If we have at least one public IP from external services, return that
		if len(publicIPs) > 0 {
			return publicIPs[0], nil
		}
		return "", err
	}

	for _, iface := range ifaces {
		// Skip loopback and interfaces that are down
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			// Skip loopback addresses
			if ip.IsLoopback() {
				continue
			}

			// Only consider IPv4 addresses for simplicity
			if ip4 := ip.To4(); ip4 != nil {
				localIPs = append(localIPs, ip4.String())
			}
		}
	}

	// Return results
	if len(publicIPs) > 0 {
		return publicIPs[0], nil
	}

	if len(localIPs) > 0 {
		return strings.Join(localIPs, ","), nil
	}

	return "", fmt.Errorf("unable to determine server IP")
}

// Helper function to check domain against multiple IPs
func checkDomainIPMatch(domain string, serverIPs string) (bool, string) {
	ips, err := net.LookupIP(domain)
	if err != nil || len(ips) == 0 {
		return false, ""
	}

	// Convert comma-separated IPs to slice
	serverIPList := strings.Split(serverIPs, ",")

	var domainIPStrings []string
	for _, ip := range ips {
		ipStr := ip.String()
		domainIPStrings = append(domainIPStrings, ipStr)

		// Check if this domain IP matches any server IP
		for _, serverIP := range serverIPList {
			if ipStr == serverIP {
				return true, ipStr
			}
		}
	}

	// No match found, return false and the domain IPs
	return false, strings.Join(domainIPStrings, ", ")
}

// CollectFromUser gets required user input upfront
func (c *Config) CollectFromUser(reader *bufio.Reader) error {
	// Check if we're in non-interactive mode
	if os.Getenv("NONINTERACTIVE") == "1" {
		return c.collectFromEnvironment()
	}

	// Initialize default values
	c.data.Domain = ""
	c.data.InstallDir = "/opt/infinity-metrics"

	// Collect domain
	for {
		fmt.Print("Enter your domain name (e.g., analytics.example.com): ")
		domain, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read domain: %w", err)
		}
		c.data.Domain = strings.TrimSpace(domain)
		if c.data.Domain == "" {
			fmt.Println("Error: Domain cannot be empty.")
			continue
		}

		// Validate domain format immediately using the same validation that will be used during installation
		if err := validation.ValidateDomain(c.data.Domain); err != nil {
			fmt.Printf("Error: %s\n", err.Error())
			continue
		}
		break
	}

	// Check DNS records and store warnings instead of blocking
	c.CheckDNSAndStoreWarnings(c.data.Domain)

	c.data.BackupPath = filepath.Join(c.data.InstallDir, "storage", "backups")

	// Show configuration summary and get confirmation
	for {
		fmt.Println("\nConfiguration Summary:")
		fmt.Printf("Domain: %s\n", c.data.Domain)
		if c.HasDNSWarnings() {
			fmt.Printf("DNS Status: ⚠️  Warnings detected (installation will continue)\n")
		} else {
			fmt.Printf("DNS Status: ✅ Verified\n")
		}
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
		// Reset all values and start over
		c.data.Domain = ""
		return c.CollectFromUser(reader)
	}

	c.logger.Success("Configuration collected from user")
	return nil
}

// collectFromEnvironment reads configuration from environment variables
func (c *Config) collectFromEnvironment() error {
	c.logger.Info("Running in non-interactive mode, reading configuration from environment variables")

	// Read domain from environment
	domain := os.Getenv("DOMAIN")
	if domain == "" {
		return fmt.Errorf("DOMAIN environment variable is required in non-interactive mode")
	}
	c.data.Domain = domain

	c.logger.Info("Configuration loaded from environment variables:")
	c.logger.Info("  Domain: %s", c.data.Domain)

	// Set default values for other fields
	c.data.InstallDir = "/opt/infinity-metrics"
	c.data.BackupPath = filepath.Join(c.data.InstallDir, "backups")
	c.data.AppImage = "karloscodes/infinity-metrics-beta:latest"
	c.data.CaddyImage = "caddy:2.7-alpine"

	return nil
}

// Helper function to format IPs for display
func formatIPs(ips []net.IP) string {
	ipStrings := make([]string, len(ips))
	for i, ip := range ips {
		ipStrings[i] = ip.String()
	}
	return strings.Join(ipStrings, ", ")
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
		case "INFINITY_METRICS_USER":
			c.data.User = value
		case "INFINITY_METRICS_LICENSE_KEY":
			c.data.LicenseKey = value
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
	fmt.Fprintf(file, "APP_IMAGE=%s\n", c.data.AppImage)
	fmt.Fprintf(file, "CADDY_IMAGE=%s\n", c.data.CaddyImage)
	fmt.Fprintf(file, "INSTALL_DIR=%s\n", c.data.InstallDir)
	fmt.Fprintf(file, "BACKUP_PATH=%s\n", c.data.BackupPath)
	fmt.Fprintf(file, "VERSION=%s\n", c.data.Version)
	fmt.Fprintf(file, "INSTALLER_URL=%s\n", c.data.InstallerURL)
	fmt.Fprintf(file, "INFINITY_METRICS_PRIVATE_KEY=%s\n", c.data.PrivateKey)
	if c.data.User != "" {
		fmt.Fprintf(file, "INFINITY_METRICS_USER=%s\n", c.data.User)
	}
	if c.data.LicenseKey != "" {
		fmt.Fprintf(file, "INFINITY_METRICS_LICENSE_KEY=%s\n", c.data.LicenseKey)
	}

	c.logger.Info("Configuration saved to %s", filename)
	return nil
}

// GetData returns the config data
func (c *Config) GetData() ConfigData {
	return c.data
}

// SetData updates the config data
func (c *Config) SetData(data ConfigData) {
	c.data = data
}

// SetCaddyImage sets the CaddyImage field in ConfigData
func (c *Config) SetCaddyImage(image string) {
	c.data.CaddyImage = image
	c.logger.Info("CaddyImage updated to: %s", image)
}

// DockerImages contains both app and caddy image information
type DockerImages struct {
	AppImage   string
	CaddyImage string
}

// GetDockerImages returns both Docker images in a structured way
func (c *Config) GetDockerImages() DockerImages {
	return DockerImages{
		AppImage:   c.data.AppImage,
		CaddyImage: c.data.CaddyImage,
	}
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
	// Validate domain
	if err := validation.ValidateDomain(c.data.Domain); err != nil {
		return errors.NewConfigError("domain", c.data.Domain, err.Error())
	}

	// Validate app image
	if c.data.AppImage == "" {
		return errors.NewConfigError("app_image", "", "app image cannot be empty")
	}

	// Validate caddy image
	if c.data.CaddyImage == "" {
		return errors.NewConfigError("caddy_image", "", "caddy image cannot be empty")
	}

	// Validate install directory path
	if err := validation.ValidateFilePath(c.data.InstallDir); err != nil {
		return errors.NewConfigError("install_dir", c.data.InstallDir, err.Error())
	}

	// Validate backup path
	if err := validation.ValidateFilePath(c.data.BackupPath); err != nil {
		return errors.NewConfigError("backup_path", c.data.BackupPath, err.Error())
	}

	// Validate private key (basic check)
	if c.data.PrivateKey == "" {
		return errors.NewConfigError("private_key", "", "private key cannot be empty")
	}
	if len(c.data.PrivateKey) < 32 {
		return errors.NewConfigError("private_key", "", "private key too short (minimum 32 characters)")
	}

	// Validate version if provided
	if c.data.Version != "" {
		if err := validation.ValidateVersion(c.data.Version); err != nil {
			return errors.NewConfigError("version", c.data.Version, err.Error())
		}
	}

	// Validate installer URL if provided
	if c.data.InstallerURL != "" {
		if err := validation.ValidateURL(c.data.InstallerURL); err != nil {
			return errors.NewConfigError("installer_url", c.data.InstallerURL, err.Error())
		}
	}

	return nil
}

// CheckDNSAndStoreWarnings checks DNS configuration and stores warnings instead of blocking
func (c *Config) CheckDNSAndStoreWarnings(domain string) {
	// Skip DNS checks for localhost - no DNS resolution needed
	if isLocalhostDomain(domain) {
		fmt.Printf("🏠 Skipping DNS checks for localhost domain: %s\n", domain)
		c.data.DNSWarnings = []string{}
		return
	}

	fmt.Printf("🔍 Checking DNS configuration for %s...\n", domain)

	// Clear any existing warnings
	c.data.DNSWarnings = []string{}

	ips, err := net.LookupIP(domain)
	if err != nil {
		warning := fmt.Sprintf("DNS lookup failed for %s: %v", domain, err)
		c.data.DNSWarnings = append(c.data.DNSWarnings, warning)
		c.data.DNSWarnings = append(c.data.DNSWarnings, "Suggestion: Check that your domain is registered and DNS is configured correctly")
		c.data.DNSWarnings = append(c.data.DNSWarnings, "Suggestion: Verify your DNS records using https://dnschecker.org/")
		return
	}

	if len(ips) == 0 {
		warning := "No A/AAAA records found for domain " + domain
		c.data.DNSWarnings = append(c.data.DNSWarnings, warning)
		c.data.DNSWarnings = append(c.data.DNSWarnings, "DNS propagation may take from a few minutes to hours to complete")
		c.data.DNSWarnings = append(c.data.DNSWarnings, "You can check DNS records at https://mxtoolbox.com/SuperTool.aspx")
		return
	}

	// Check if domain resolves to server IP
	serverIPs, err := getCurrentServerIP()
	if err != nil {
		warning := fmt.Sprintf("Could not determine server IP addresses: %v", err)
		c.data.DNSWarnings = append(c.data.DNSWarnings, warning)
		c.data.DNSWarnings = append(c.data.DNSWarnings, fmt.Sprintf("Domain %s resolves to: %s", domain, formatIPs(ips)))
		c.data.DNSWarnings = append(c.data.DNSWarnings, "Please verify manually that one of these IPs matches this server")
	} else {
		match, matchedIP := checkDomainIPMatch(domain, serverIPs)
		if !match {
			warning := fmt.Sprintf("Domain %s does not resolve to this server", domain)
			c.data.DNSWarnings = append(c.data.DNSWarnings, warning)
			c.data.DNSWarnings = append(c.data.DNSWarnings, fmt.Sprintf("Server IP(s): %s", serverIPs))
			c.data.DNSWarnings = append(c.data.DNSWarnings, fmt.Sprintf("Domain resolves to: %s", formatIPs(ips)))
			c.data.DNSWarnings = append(c.data.DNSWarnings, "Update your domain's DNS records to point to this server's IP")
		} else {
			fmt.Printf("✅ DNS configuration verified: %s resolves to server IP %s\n", domain, matchedIP)
		}
	}

	// Display warnings if any exist
	if len(c.data.DNSWarnings) > 0 {
		c.displayDNSWarnings()
	}
}

// displayDNSWarnings shows DNS configuration warnings to the user
func (c *Config) displayDNSWarnings() {
	fmt.Println("\n⚠️  DNS Configuration Warnings:")
	for _, warning := range c.data.DNSWarnings {
		if strings.HasPrefix(warning, "Suggestion:") {
			fmt.Printf("   💡 %s\n", warning[11:]) // Remove "Suggestion:" prefix
		} else {
			fmt.Printf("   • %s\n", warning)
		}
	}
	fmt.Printf("\n📋 Installation will continue, but you may need to fix DNS issues for external access.\n\n")
}

// GetDNSWarnings returns the current DNS warnings
func (c *Config) GetDNSWarnings() []string {
	return c.data.DNSWarnings
}

// HasDNSWarnings returns true if there are DNS configuration warnings
func (c *Config) HasDNSWarnings() bool {
	return len(c.data.DNSWarnings) > 0
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

// readPassword reads a password from either terminal or stdin based on environment
func (c *Config) readPassword(reader *bufio.Reader, prompt string) (string, error) {
	fmt.Print(prompt)

	var passwordBytes []byte
	var err error

	// In test environment, read password from stdin instead of terminal
	if os.Getenv("ENV") == "test" {
		password, readErr := reader.ReadString('\n')
		if readErr != nil {
			err = readErr
		} else {
			passwordBytes = []byte(strings.TrimSpace(password))
		}
	} else {
		passwordBytes, err = term.ReadPassword(int(syscall.Stdin))
	}

	if err != nil {
		return "", fmt.Errorf("failed to read password: %w", err)
	}

	if os.Getenv("ENV") != "test" {
		fmt.Println() // Only add newline for terminal mode
	}

	return strings.TrimSpace(string(passwordBytes)), nil
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
	// Try new naming pattern first (infinity-metrics-installer)
	binaryNameNew := fmt.Sprintf("infinity-metrics-installer-v%s-%s", version, runtime.GOARCH)
	// Fallback to old naming pattern for backwards compatibility
	binaryNameOld := fmt.Sprintf("infinity-metrics-v%s-%s", version, runtime.GOARCH)
	var binaryURL string
	var foundPattern string

	for _, asset := range release.Assets {
		switch asset.Name {
		case "config.json":
			configURL = asset.DownloadURL
		case binaryNameNew:
			binaryURL = asset.DownloadURL
			foundPattern = "new"
		case binaryNameOld:
			// Only use old pattern if new pattern wasn't found
			if binaryURL == "" {
				binaryURL = asset.DownloadURL
				foundPattern = "old"
			}
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
		if foundPattern != "" {
			c.logger.Info("Found binary using %s naming pattern", foundPattern)
		}
	} else {
		c.logger.Warn("Binary %s not found in latest release, keeping default URL", binaryNameNew)
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

// isLocalhostDomain checks if the domain is localhost or a localhost variant
func isLocalhostDomain(domain string) bool {
	// Check for common localhost variants
	localhostDomains := []string{
		"localhost",
		"127.0.0.1",
		"::1",
		"0.0.0.0",
		"localhost.localdomain",
	}

	domain = strings.ToLower(strings.TrimSpace(domain))
	for _, localhost := range localhostDomains {
		if domain == localhost {
			return true
		}
	}

	// Check for localhost with port (e.g., localhost:8080)
	if strings.HasPrefix(domain, "localhost:") {
		return true
	}

	// Check for localhost subdomains (e.g., app.localhost, test.localhost)
	if strings.HasSuffix(domain, ".localhost") {
		return true
	}

	return false
}

