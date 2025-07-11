package installer

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"infinity-metrics-installer/internal/config"
	"infinity-metrics-installer/internal/logging"
)

func TestNewInstaller(t *testing.T) {
	logger := logging.NewLogger(logging.Config{Level: "error", Quiet: true})
	installer := NewInstaller(logger)

	assert.NotNil(t, installer)
	assert.NotNil(t, installer.logger)
	assert.NotNil(t, installer.config)
	assert.NotNil(t, installer.docker)
	assert.NotNil(t, installer.database)
	assert.Equal(t, DefaultBinaryPath, installer.binaryPath)
}

func TestGetConfig(t *testing.T) {
	logger := logging.NewLogger(logging.Config{Level: "error", Quiet: true})
	installer := NewInstaller(logger)

	config := installer.GetConfig()
	assert.NotNil(t, config)
}

func TestGetMainDBPath(t *testing.T) {
	logger := logging.NewLogger(logging.Config{Level: "error", Quiet: true})
	installer := NewInstaller(logger)

	// Test with default config
	dbPath := installer.GetMainDBPath()
	expectedPath := filepath.Join(DefaultInstallDir, "storage", "infinity-metrics-production.db")
	assert.Equal(t, expectedPath, dbPath)

	// Test with custom install dir
	cfg := config.NewConfig(logger)
	cfg.SetInstallDir("/custom/install/dir")
	installer.config = cfg

	dbPath = installer.GetMainDBPath()
	expectedPath = filepath.Join("/custom/install/dir", "storage", "infinity-metrics-production.db")
	assert.Equal(t, expectedPath, dbPath)
}

func TestGetBackupDir(t *testing.T) {
	logger := logging.NewLogger(logging.Config{Level: "error", Quiet: true})
	installer := NewInstaller(logger)

	// Test with default config
	backupDir := installer.GetBackupDir()
	expectedDir := filepath.Join(DefaultInstallDir, "storage", "backups")
	assert.Equal(t, expectedDir, backupDir)

	// Test with custom install dir
	cfg := config.NewConfig(logger)
	cfg.SetInstallDir("/custom/install/dir")
	installer.config = cfg

	backupDir = installer.GetBackupDir()
	expectedDir = filepath.Join("/custom/install/dir", "storage", "backups")
	assert.Equal(t, expectedDir, backupDir)
}

func TestConstants(t *testing.T) {
	assert.Equal(t, "/opt/infinity-metrics", DefaultInstallDir)
	assert.Equal(t, "/usr/local/bin/infinity-metrics", DefaultBinaryPath)
	assert.Equal(t, "/etc/cron.d/infinity-metrics-update", DefaultCronFile)
	assert.Equal(t, "0 3 * * *", DefaultCronSchedule)
}

// Note: RunWithConfig, Run, Restore, and VerifyInstallation require more complex
// setup with Docker and file system mocking, which would be better suited for
// integration tests rather than unit tests.
