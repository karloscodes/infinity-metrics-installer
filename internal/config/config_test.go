package config

import (
	"os"
	"strings"
	"testing"

	"infinity-metrics-installer/internal/logging"
)

func testLogger(t *testing.T) *logging.Logger {
	dir := t.TempDir()
	logger := logging.NewLogger(logging.Config{LogDir: dir})
	return logger
}

func TestValidate_AllFieldsPresent(t *testing.T) {
	c := NewConfig(testLogger(t))
	c.SetInstallDir("/test/dir")
	c.SetCaddyImage("caddy:test")
	c.SetInstallerURL("http://example.com")
	c.data.Domain = "example.com"
	c.data.AdminEmail = "admin@example.com"
	c.data.LicenseKey = "valid-license-key-123"
	c.data.AdminPassword = "ValidPass123!"
	c.data.AppImage = "appimg"
	c.data.CaddyImage = "caddyimg"
	c.data.InstallDir = "/test/dir"
	c.data.BackupPath = "/backup"
	c.data.PrivateKey = "this-is-a-very-long-private-key-that-meets-minimum-requirements"
	c.data.Version = "v1.0.0"
	c.data.InstallerURL = "https://example.com/installer"
	// Test Validate
	err := c.Validate()
	if err != nil {
		t.Errorf("Validate() returned error with all fields present: %v", err)
	}
}

func TestValidate_MissingFields(t *testing.T) {
	fields := []struct {
		name    string
		modify  func(c *Config)
		wantErr string
	}{
		{"Domain", func(c *Config) { c.data.Domain = "" }, "config error for field 'domain': validation failed for field 'domain': domain cannot be empty"},
		{"AdminEmail", func(c *Config) { c.data.AdminEmail = "" }, "config error for field 'admin_email': validation failed for field 'email': email cannot be empty"},
		{"LicenseKey", func(c *Config) { c.data.LicenseKey = "" }, "config error for field 'license_key': validation failed for field 'license': license key cannot be empty"},
		{"AppImage", func(c *Config) { c.data.AppImage = "" }, "config error for field 'app_image': app image cannot be empty"},
		{"CaddyImage", func(c *Config) { c.data.CaddyImage = "" }, "config error for field 'caddy_image': caddy image cannot be empty"},
		{"InstallDir", func(c *Config) { c.data.InstallDir = "" }, "config error for field 'install_dir': validation failed for field 'file_path': file path cannot be empty"},
		{"BackupPath", func(c *Config) { c.data.BackupPath = "" }, "config error for field 'backup_path': validation failed for field 'file_path': file path cannot be empty"},
		{"PrivateKey", func(c *Config) { c.data.PrivateKey = "" }, "config error for field 'private_key': private key cannot be empty"},
	}
	for _, tc := range fields {
		t.Run(tc.name, func(t *testing.T) {
			c := NewConfig(testLogger(t))
			c.data.Domain = "example.com"
			c.data.AdminEmail = "admin@example.com"
			c.data.LicenseKey = "valid-license-key-123"
			c.data.AdminPassword = "ValidPass123!"
			c.data.AppImage = "appimg"
			c.data.CaddyImage = "caddyimg"
			c.data.InstallDir = "/test/dir"
			c.data.BackupPath = "/backup"
			c.data.PrivateKey = "this-is-a-very-long-private-key-that-meets-minimum-requirements"
			c.data.Version = "v1.0.0"
			c.data.InstallerURL = "https://example.com/installer"
			tc.modify(c)
			err := c.Validate()
			if err == nil || err.Error() != tc.wantErr {
				t.Errorf("Validate() missing %s: got %v, want %q", tc.name, err, tc.wantErr)
			}
		})
	}
}

func TestSettersAndGetters(t *testing.T) {
	c := NewConfig(testLogger(t))
	c.SetInstallDir("/foo/bar")
	if got := c.GetMainDBPath(); got != "/foo/bar/storage/infinity-metrics-production.db" {
		t.Errorf("GetMainDBPath() = %q, want %q", got, "/foo/bar/storage/infinity-metrics-production.db")
	}
	c.SetCaddyImage("caddy:custom")
	if c.data.CaddyImage != "caddy:custom" {
		t.Errorf("SetCaddyImage() did not update field")
	}
	c.SetInstallerURL("http://installer")
	if c.data.InstallerURL != "http://installer" {
		t.Errorf("SetInstallerURL() did not update field")
	}
}

func TestGeneratePrivateKey_Uniqueness(t *testing.T) {
	keys := make(map[string]bool)
	for i := 0; i < 100; i++ {
		key, err := generatePrivateKey()
		if err != nil {
			t.Fatalf("generatePrivateKey() error: %v", err)
		}
		if len(key) != 32 {
			t.Errorf("generatePrivateKey() length = %d, want 32", len(key))
		}
		if keys[key] {
			t.Errorf("generatePrivateKey() produced duplicate key: %s", key)
		}
		keys[key] = true
	}
}

func TestNewConfig_Defaults(t *testing.T) {
	c := NewConfig(testLogger(t))
	data := c.data
	if data.AppImage != "karloscodes/infinity-metrics-beta:latest" {
		t.Errorf("AppImage default = %q, want %q", data.AppImage, "karloscodes/infinity-metrics-beta:latest")
	}
	if data.CaddyImage != "caddy:2.7-alpine" {
		t.Errorf("CaddyImage default = %q, want %q", data.CaddyImage, "caddy:2.7-alpine")
	}
	if data.InstallDir != "/opt/infinity-metrics" {
		t.Errorf("InstallDir default = %q, want %q", data.InstallDir, "/opt/infinity-metrics")
	}
}

func TestGetData(t *testing.T) {
	c := NewConfig(testLogger(t))
	c.data.Domain = "test.example.com"
	c.data.AdminEmail = "admin@test.com"
	c.data.LicenseKey = "test-license"
	c.data.AdminPassword = "test-password"

	data := c.GetData()
	if data.Domain != "test.example.com" {
		t.Errorf("GetData().Domain = %q, want %q", data.Domain, "test.example.com")
	}
	if data.AdminEmail != "admin@test.com" {
		t.Errorf("GetData().AdminEmail = %q, want %q", data.AdminEmail, "admin@test.com")
	}
	if data.LicenseKey != "test-license" {
		t.Errorf("GetData().LicenseKey = %q, want %q", data.LicenseKey, "test-license")
	}
	if data.AdminPassword != "test-password" {
		t.Errorf("GetData().AdminPassword = %q, want %q", data.AdminPassword, "test-password")
	}
}

func TestGetDockerImages(t *testing.T) {
	c := NewConfig(testLogger(t))
	c.data.AppImage = "custom/app:1.0"
	c.data.CaddyImage = "custom/caddy:2.0"

	images := c.GetDockerImages()
	if images.AppImage != "custom/app:1.0" {
		t.Errorf("GetDockerImages().AppImage = %q, want %q", images.AppImage, "custom/app:1.0")
	}
	if images.CaddyImage != "custom/caddy:2.0" {
		t.Errorf("GetDockerImages().CaddyImage = %q, want %q", images.CaddyImage, "custom/caddy:2.0")
	}
}

func TestLoadFromFile(t *testing.T) {
	// Test loading valid .env file
	t.Run("ValidFile", func(t *testing.T) {
		c := NewConfig(testLogger(t))

		// Create temp file with config data
		tmpFile := t.TempDir() + "/test.env"
		content := `INFINITY_METRICS_DOMAIN=test.example.com
INFINITY_METRICS_ADMIN_EMAIL=admin@test.com
INFINITY_METRICS_LICENSE_KEY=test-license-key
APP_IMAGE=test/app:latest
CADDY_IMAGE=test/caddy:latest
INSTALL_DIR=/custom/install
BACKUP_PATH=/custom/backup
VERSION=1.2.3
INSTALLER_URL=https://test.com/installer
INFINITY_METRICS_PRIVATE_KEY=testprivatekey123
`
		if err := os.WriteFile(tmpFile, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}

		err := c.LoadFromFile(tmpFile)
		if err != nil {
			t.Errorf("LoadFromFile() error = %v", err)
		}

		// Verify loaded values
		if c.data.Domain != "test.example.com" {
			t.Errorf("Domain = %q, want %q", c.data.Domain, "test.example.com")
		}
		if c.data.AdminEmail != "admin@test.com" {
			t.Errorf("AdminEmail = %q, want %q", c.data.AdminEmail, "admin@test.com")
		}
		if c.data.LicenseKey != "test-license-key" {
			t.Errorf("LicenseKey = %q, want %q", c.data.LicenseKey, "test-license-key")
		}
		if c.data.AppImage != "test/app:latest" {
			t.Errorf("AppImage = %q, want %q", c.data.AppImage, "test/app:latest")
		}
		if c.data.CaddyImage != "test/caddy:latest" {
			t.Errorf("CaddyImage = %q, want %q", c.data.CaddyImage, "test/caddy:latest")
		}
		if c.data.InstallDir != "/custom/install" {
			t.Errorf("InstallDir = %q, want %q", c.data.InstallDir, "/custom/install")
		}
		if c.data.BackupPath != "/custom/backup" {
			t.Errorf("BackupPath = %q, want %q", c.data.BackupPath, "/custom/backup")
		}
		if c.data.Version != "1.2.3" {
			t.Errorf("Version = %q, want %q", c.data.Version, "1.2.3")
		}
		if c.data.InstallerURL != "https://test.com/installer" {
			t.Errorf("InstallerURL = %q, want %q", c.data.InstallerURL, "https://test.com/installer")
		}
		if c.data.PrivateKey != "testprivatekey123" {
			t.Errorf("PrivateKey = %q, want %q", c.data.PrivateKey, "testprivatekey123")
		}
	})

	// Test missing private key generation
	t.Run("MissingPrivateKey", func(t *testing.T) {
		c := NewConfig(testLogger(t))

		tmpFile := t.TempDir() + "/test.env"
		content := `INFINITY_METRICS_DOMAIN=test.example.com
INFINITY_METRICS_ADMIN_EMAIL=admin@test.com
`
		if err := os.WriteFile(tmpFile, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}

		err := c.LoadFromFile(tmpFile)
		if err != nil {
			t.Errorf("LoadFromFile() error = %v", err)
		}

		// Should have generated a private key
		if c.data.PrivateKey == "" {
			t.Error("PrivateKey should be generated when missing")
		}
		if len(c.data.PrivateKey) != 32 {
			t.Errorf("Generated PrivateKey length = %d, want 32", len(c.data.PrivateKey))
		}
	})

	// Test nonexistent file
	t.Run("NonexistentFile", func(t *testing.T) {
		c := NewConfig(testLogger(t))
		err := c.LoadFromFile("/nonexistent/file.env")
		if err == nil {
			t.Error("LoadFromFile() should return error for nonexistent file")
		}
	})
}

func TestSaveToFile(t *testing.T) {
	c := NewConfig(testLogger(t))
	c.data.Domain = "save.example.com"
	c.data.AdminEmail = "save@test.com"
	c.data.LicenseKey = "save-license"
	c.data.AppImage = "save/app:latest"
	c.data.CaddyImage = "save/caddy:latest"
	c.data.InstallDir = "/save/install"
	c.data.BackupPath = "/save/backup"
	c.data.Version = "2.0.0"
	c.data.InstallerURL = "https://save.com/installer"

	tmpFile := t.TempDir() + "/save.env"

	err := c.SaveToFile(tmpFile)
	if err != nil {
		t.Errorf("SaveToFile() error = %v", err)
	}

	// Read the file back and verify content
	content, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatal(err)
	}

	expected := []string{
		"INFINITY_METRICS_DOMAIN=save.example.com",
		"INFINITY_METRICS_ADMIN_EMAIL=save@test.com",
		"INFINITY_METRICS_LICENSE_KEY=save-license",
		"APP_IMAGE=save/app:latest",
		"CADDY_IMAGE=save/caddy:latest",
		"INSTALL_DIR=/save/install",
		"BACKUP_PATH=/save/backup",
		"VERSION=2.0.0",
		"INSTALLER_URL=https://save.com/installer",
		"INFINITY_METRICS_PRIVATE_KEY=", // Should be generated
	}

	contentStr := string(content)
	for _, expectedLine := range expected {
		if !strings.Contains(contentStr, expectedLine) {
			t.Errorf("SaveToFile() missing expected line: %s", expectedLine)
		}
	}

	// Verify private key was generated and saved
	if !strings.Contains(contentStr, "INFINITY_METRICS_PRIVATE_KEY=") {
		t.Error("SaveToFile() should include INFINITY_METRICS_PRIVATE_KEY")
	}
}

func TestDNSWarnings(t *testing.T) {
	c := NewConfig(testLogger(t))

	// Initially no warnings
	if c.HasDNSWarnings() {
		t.Error("HasDNSWarnings() should be false initially")
	}
	if len(c.GetDNSWarnings()) != 0 {
		t.Error("GetDNSWarnings() should be empty initially")
	}

	// Add some warnings manually for testing
	c.data.DNSWarnings = []string{"Warning 1", "Warning 2"}

	if !c.HasDNSWarnings() {
		t.Error("HasDNSWarnings() should be true after adding warnings")
	}

	warnings := c.GetDNSWarnings()
	if len(warnings) != 2 {
		t.Errorf("GetDNSWarnings() length = %d, want 2", len(warnings))
	}
	if warnings[0] != "Warning 1" {
		t.Errorf("GetDNSWarnings()[0] = %q, want %q", warnings[0], "Warning 1")
	}
	if warnings[1] != "Warning 2" {
		t.Errorf("GetDNSWarnings()[1] = %q, want %q", warnings[1], "Warning 2")
	}
}

func TestCheckDNSAndStoreWarnings(t *testing.T) {
	c := NewConfig(testLogger(t))

	// Test with invalid domain (should generate warnings)
	c.CheckDNSAndStoreWarnings("invalid-domain-that-does-not-exist.nonexistent")

	if !c.HasDNSWarnings() {
		t.Error("CheckDNSAndStoreWarnings() should generate warnings for invalid domain")
	}

	warnings := c.GetDNSWarnings()
	if len(warnings) == 0 {
		t.Error("CheckDNSAndStoreWarnings() should add warnings for invalid domain")
	}

	// Verify that at least one warning mentions DNS lookup failure
	found := false
	for _, warning := range warnings {
		if strings.Contains(warning, "DNS lookup failed") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected DNS lookup failure warning not found")
	}
}

func TestCollectFromEnvironment(t *testing.T) {
	c := NewConfig(testLogger(t))

	// Set environment variables
	originalEnv := make(map[string]string)
	envVars := map[string]string{
		"NONINTERACTIVE": "1",
		"DOMAIN":         "env.example.com",
		"ADMIN_EMAIL":    "env@test.com",
		"LICENSE_KEY":    "env-license",
		"ADMIN_PASSWORD": "env-password",
	}

	// Backup and set environment variables
	for key, value := range envVars {
		originalEnv[key] = os.Getenv(key)
		os.Setenv(key, value)
	}

	// Restore environment after test
	defer func() {
		for key, original := range originalEnv {
			if original == "" {
				os.Unsetenv(key)
			} else {
				os.Setenv(key, original)
			}
		}
	}()

	// Test successful collection
	err := c.collectFromEnvironment()
	if err != nil {
		t.Errorf("collectFromEnvironment() error = %v", err)
	}

	if c.data.Domain != "env.example.com" {
		t.Errorf("Domain = %q, want %q", c.data.Domain, "env.example.com")
	}
	if c.data.AdminEmail != "env@test.com" {
		t.Errorf("AdminEmail = %q, want %q", c.data.AdminEmail, "env@test.com")
	}
	if c.data.LicenseKey != "env-license" {
		t.Errorf("LicenseKey = %q, want %q", c.data.LicenseKey, "env-license")
	}
	if c.data.AdminPassword != "env-password" {
		t.Errorf("AdminPassword = %q, want %q", c.data.AdminPassword, "env-password")
	}

	// Test missing required variable
	os.Unsetenv("DOMAIN")
	err = c.collectFromEnvironment()
	if err == nil {
		t.Error("collectFromEnvironment() should return error when DOMAIN is missing")
	}
}

func TestFetchFromServer(t *testing.T) {
	c := NewConfig(testLogger(t))

	// Test with invalid URL (should not fail, just warn and continue)
	err := c.FetchFromServer("https://invalid-url-that-does-not-exist.com")
	if err != nil {
		t.Errorf("FetchFromServer() should not fail on network errors, got: %v", err)
	}

	// Test with empty URL (uses default GitHub API)
	err = c.FetchFromServer("")
	if err != nil {
		t.Errorf("FetchFromServer() with empty URL should not fail, got: %v", err)
	}
}
