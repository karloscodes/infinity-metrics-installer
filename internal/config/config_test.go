package config

import (
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
	c.data.LicenseKey = "key"
	c.data.AdminPassword = "pass"
	c.data.AppImage = "appimg"
	c.data.CaddyImage = "caddyimg"
	c.data.InstallDir = "/test/dir"
	c.data.BackupPath = "/backup"
	c.data.PrivateKey = "privkey"
	c.data.Version = "1.0"
	c.data.InstallerURL = "url"
	// Test Validate
	err := c.Validate()
	if err != nil {
		t.Errorf("Validate() returned error with all fields present: %v", err)
	}
}

func TestValidate_MissingFields(t *testing.T) {
	fields := []struct {
		name string
		modify func(c *Config)
		wantErr string
	}{
		{"Domain", func(c *Config) { c.data.Domain = "" }, "domain is required"},
		{"AdminEmail", func(c *Config) { c.data.AdminEmail = "" }, "admin email is required"},
		{"LicenseKey", func(c *Config) { c.data.LicenseKey = "" }, "license key is required"},
		{"AdminPassword", func(c *Config) { c.data.AdminPassword = "" }, "password is required"},
		{"AppImage", func(c *Config) { c.data.AppImage = "" }, "app image is required"},
		{"CaddyImage", func(c *Config) { c.data.CaddyImage = "" }, "caddy image is required"},
		{"InstallDir", func(c *Config) { c.data.InstallDir = "" }, "install directory is required"},
		{"BackupPath", func(c *Config) { c.data.BackupPath = "" }, "backup path is required"},
		{"PrivateKey", func(c *Config) { c.data.PrivateKey = "" }, "private key is required"},
	}
	for _, tc := range fields {
		t.Run(tc.name, func(t *testing.T) {
			c := NewConfig(testLogger(t))
			c.data.Domain = "example.com"
			c.data.AdminEmail = "admin@example.com"
			c.data.LicenseKey = "key"
			c.data.AdminPassword = "pass"
			c.data.AppImage = "appimg"
			c.data.CaddyImage = "caddyimg"
			c.data.InstallDir = "/test/dir"
			c.data.BackupPath = "/backup"
			c.data.PrivateKey = "privkey"
			c.data.Version = "1.0"
			c.data.InstallerURL = "url"
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
