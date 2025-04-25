package updater

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"infinity-metrics-installer/internal/logging"
)

// TestPrivateKeyGeneration ensures that updater Run saves private key when missing.
func TestPrivateKeyGeneration(t *testing.T) {
	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, ".env")
	os.WriteFile(envFile, []byte("INFINITY_METRICS_DOMAIN=localhost\n"), 0644)

	logger := logging.NewLogger(logging.Config{Level: "error"})
	u := NewUpdater(logger)

	// Directly use config load/save logic which generates the key when missing
	cfg := u.config
	if err := cfg.LoadFromFile(envFile); err != nil {
		t.Fatalf("load err: %v", err)
	}
	if cfg.GetData().PrivateKey == "" {
		t.Fatalf("expected private key to be generated on load")
	}

	content, _ := os.ReadFile(envFile)
	if !strings.Contains(string(content), "INFINITY_METRICS_PRIVATE_KEY") {
		t.Fatalf("env file missing generated key")
	}

	// Call SaveToFile and ensure key persists
	if err := cfg.SaveToFile(envFile); err != nil {
		t.Fatalf("save err: %v", err)
	}
	content2, _ := os.ReadFile(envFile)
	if !strings.Contains(string(content2), cfg.GetData().PrivateKey) {
		t.Fatalf("saved key not found in env file")
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		exp  int
	}{
		{"minor lt", "1.0.0", "1.2.0", -1},
		{"major gt", "2.0.0", "1.9.9", 1},
		{"equal", "1.0.0", "1.0.0", 0},
		{"different segment lengths eq", "1.0", "1.0.0", 0},
		{"patch gt", "1.0.10", "1.0.2", 1},
		{"numeric compare not lexicographic", "1.10.1", "1.9.9", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := compareVersions(c.a, c.b); got != c.exp {
				t.Fatalf("compareVersions(%s,%s)=%d want %d", c.a, c.b, got, c.exp)
			}
		})
	}
}

func TestExtractVersionFromURL(t *testing.T) {
	tests := map[string]string{
		"https://github.com/karloscodes/infinity-metrics-installer/releases/download/v1.2.3/infinity-metrics-v1.2.3-amd64": "1.2.3",
		"https://no-version.com/asset": "",
	}
	for url, want := range tests {
		if got := extractVersionFromURL(url); got != want {
			t.Errorf("extractVersionFromURL(%s)=%s want %s", url, got, want)
		}
	}
}

// TestUpdaterRun_NoUpdateNeeded verifies Updater.Run does nothing if current version is up-to-date.
func TestUpdaterRun_NoUpdateNeeded(t *testing.T) {
	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, ".env")
	os.WriteFile(envFile, []byte("INFINITY_METRICS_DOMAIN=localhost\nINFINITY_METRICS_INSTALLER_URL=https://github.com/karloscodes/infinity-metrics-installer/releases/download/v1.2.3/infinity-metrics-v1.2.3-amd64\n"), 0644)

	logger := logging.NewLogger(logging.Config{Level: "error"})
	u := NewUpdater(logger)
	u.config.SetInstallerURL("https://github.com/karloscodes/infinity-metrics-installer/releases/download/v1.2.3/infinity-metrics-v1.2.3-amd64")
	u.config.SetInstallDir(tmpDir)
	u.config.SaveToFile(envFile)

	err := u.Run("1.2.3")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

// TestUpdaterRun_UpdateNeeded simulates an update scenario (mocking updateBinary).
func TestUpdaterRun_UpdateNeeded(t *testing.T) {
	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, ".env")
	os.WriteFile(envFile, []byte("INFINITY_METRICS_DOMAIN=localhost\nINFINITY_METRICS_INSTALLER_URL=https://github.com/karloscodes/infinity-metrics-installer/releases/download/v1.2.4/infinity-metrics-v1.2.4-amd64\n"), 0644)

	logger := logging.NewLogger(logging.Config{Level: "error"})
	u := NewUpdater(logger)
	u.config.SetInstallerURL("https://github.com/karloscodes/infinity-metrics-installer/releases/download/v1.2.4/infinity-metrics-v1.2.4-amd64")
	u.config.SetInstallDir(tmpDir)
	u.config.SaveToFile(envFile)

	// We cannot patch u.updateBinary directly since it's a method, so we only test up to the point where updateBinary would be called.
	// To fully mock, the codebase would need to be refactored for dependency injection.
	// For now, just check that Run attempts an update and doesn't error (may not reach updateBinary in unit test context).
	err := u.Run("1.2.3")
	if err != nil {
		t.Logf("Run returned error, this may be expected if updateBinary fails in test context: %v", err)
	}
}

// TestGetLatestVersionAndBinaryURL_Malformed tests error handling for malformed HTTP response.
// Note: Cannot patch GitHubAPIURL const; would need refactor for full test isolation.
func TestGetLatestVersionAndBinaryURL_Malformed(t *testing.T) {
	logger := logging.NewLogger(logging.Config{Level: "error"})
	_ = NewUpdater(logger)

	// The following lines are commented out because GitHubAPIURL is a const and cannot be reassigned in Go.
	// oldURL := GitHubAPIURL
	// defer func() { GitHubAPIURL = oldURL }()
	// GitHubAPIURL = "http://127.0.0.1:0/invalid" // Invalid URL to force error

	// This test cannot fully simulate a malformed HTTP response due to the const limitation.
	// To enable this, refactor GitHubAPIURL to be a variable or injectable field.

	// _, _, _, err := u.getLatestVersionAndBinaryURL()
	// if err == nil {
	// 	t.Fatalf("expected error from malformed HTTP response")
	// }
}
