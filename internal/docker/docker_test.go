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
