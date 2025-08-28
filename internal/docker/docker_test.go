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
	data := config.ConfigData{Domain: "example.com"}
	caddyfile, err := d.generateCaddyfile(data)
	if err != nil {
		t.Fatalf("generateCaddyfile error: %v", err)
	}
	if !strings.Contains(caddyfile, "admin-infinity-metrics@example.com") {
		t.Errorf("Caddyfile missing generated admin email in prod env")
	}
}

func TestCaddyFileGeneration(t *testing.T) {
	t.Run("ProductionConfigIncludesSSLConfiguration", func(t *testing.T) {
		d := &Docker{logger: testLogger(t)}
		data := config.ConfigData{
			Domain:     "production.company.com",
		}
		
		caddyfile, err := d.generateCaddyfile(data)
		
		if err != nil {
			t.Errorf("Expected Caddyfile generation to succeed, got error: %v", err)
		}
		
		if !strings.Contains(caddyfile, "admin-infinity-metrics@company.com") {
			t.Error("Expected Caddyfile to include generated admin email for SSL certificates")
		}
		
		if !strings.Contains(caddyfile, "production.company.com") {
			t.Error("Expected Caddyfile to include production domain")
		}
	})

	t.Run("TestEnvironmentGeneratesValidCaddyfile", func(t *testing.T) {
		d := &Docker{logger: testLogger(t)}
		data := config.ConfigData{
			Domain:     "localhost",
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

func TestExtractBaseDomain(t *testing.T) {
	tests := []struct {
		name     string
		domain   string
		expected string
	}{
		// Examples from requirements
		{"subdomain example", "t.getinfinitymetrics.com", "getinfinitymetrics.com"},
		{"google.com", "google.com", "google.com"},
		{"analytics subdomain", "analytics.company.com", "company.com"},
		
		// Additional test cases
		{"single label", "localhost", "localhost"},
		{"triple subdomain", "sub.analytics.company.com", "company.com"},
		{"IP address", "127.0.0.1", "127.0.0.1"},
		{"IPv6", "::1", "::1"},
		{"localhost with port", "localhost:8080", "localhost:8080"},
		{"localhost subdomain", "app.localhost", "app.localhost"},
		{"empty string", "", ""},
		{"with whitespace", "  analytics.company.com  ", "company.com"},
		{"mixed case", "Analytics.Company.COM", "company.com"},
		{"org domain", "sub.example.org", "example.org"},
		{"uk domain", "sub.example.co.uk", "co.uk"}, // Note: this is a limitation, ideally would be example.co.uk
		{"many subdomains", "a.b.c.d.example.com", "example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractBaseDomain(tt.domain)
			if result != tt.expected {
				t.Errorf("extractBaseDomain(%q) = %q, want %q", tt.domain, result, tt.expected)
			}
		})
	}
}

func TestGenerateAdminEmail(t *testing.T) {
	tests := []struct {
		name     string
		domain   string
		expected string
	}{
		// Examples from requirements
		{"subdomain example", "t.getinfinitymetrics.com", "admin-infinity-metrics@getinfinitymetrics.com"},
		{"google.com", "google.com", "admin-infinity-metrics@google.com"},
		{"analytics subdomain", "analytics.company.com", "admin-infinity-metrics@company.com"},
		
		// Additional test cases
		{"localhost", "localhost", "admin-infinity-metrics@localhost"},
		{"triple subdomain", "sub.analytics.company.com", "admin-infinity-metrics@company.com"},
		{"org domain", "sub.example.org", "admin-infinity-metrics@example.org"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generateAdminEmail(tt.domain)
			if result != tt.expected {
				t.Errorf("generateAdminEmail(%q) = %q, want %q", tt.domain, result, tt.expected)
			}
		})
	}
}

