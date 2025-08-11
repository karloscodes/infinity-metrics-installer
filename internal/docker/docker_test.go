package docker

import (
	"strings"
	"testing"

	"infinity-metrics-installer/internal/config"
	"infinity-metrics-installer/internal/logging"
)

func testLogger(t *testing.T) *logging.Logger {
	dir := t.TempDir()
	return logging.NewLogger(logging.Config{LogDir: dir})
}

func TestGenerateCaddyfile_ProdEnv(t *testing.T) {
	d := &Docker{logger: testLogger(t)}
	data := config.ConfigData{Domain: "example.com", AdminEmail: "admin@example.com"}
	caddyfile, err := d.generateCaddyfile(data)
	if err != nil {
		t.Fatalf("generateCaddyfile error: %v", err)
	}
	if !strings.Contains(caddyfile, "admin@example.com") {
		t.Errorf("Caddyfile missing admin email in prod env")
	}
}

func TestCaddyFileGeneration(t *testing.T) {
	t.Run("ProductionConfigIncludesSSLConfiguration", func(t *testing.T) {
		d := &Docker{logger: testLogger(t)}
		data := config.ConfigData{
			Domain:     "production.company.com",
			AdminEmail: "admin@company.com",
		}
		
		caddyfile, err := d.generateCaddyfile(data)
		
		if err != nil {
			t.Errorf("Expected Caddyfile generation to succeed, got error: %v", err)
		}
		
		if !strings.Contains(caddyfile, "admin@company.com") {
			t.Error("Expected Caddyfile to include admin email for SSL certificates")
		}
		
		if !strings.Contains(caddyfile, "production.company.com") {
			t.Error("Expected Caddyfile to include production domain")
		}
	})

	t.Run("TestEnvironmentGeneratesValidCaddyfile", func(t *testing.T) {
		d := &Docker{logger: testLogger(t)}
		data := config.ConfigData{
			Domain:     "localhost",
			AdminEmail: "test@localhost",
		}
		
		caddyfile, err := d.generateCaddyfile(data)
		
		if err != nil {
			t.Errorf("Expected Caddyfile generation to succeed in test env, got error: %v", err)
		}
		
		if !strings.Contains(caddyfile, "localhost") {
			t.Error("Expected Caddyfile to include localhost domain for testing")
		}
		
		// Should still contain basic configuration
		if len(caddyfile) == 0 {
			t.Error("Expected non-empty Caddyfile even in test environment")
		}
	})
}

