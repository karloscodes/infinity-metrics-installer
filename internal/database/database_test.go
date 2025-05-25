package database

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"infinity-metrics-installer/internal/logging"
)

func TestNewDatabase(t *testing.T) {
	db := NewDatabase(nil)
	if db == nil {
		t.Fatal("NewDatabase returned nil")
	}
}

func TestListBackups_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	db := NewDatabase(nil)
	backups, err := db.ListBackups(dir)
	if err != nil {
		t.Fatalf("ListBackups error: %v", err)
	}
	if len(backups) != 0 {
		t.Errorf("Expected 0 backups, got %d", len(backups))
	}
}

func TestListBackups_Sorted(t *testing.T) {
	dir := t.TempDir()
	files := []string{"backup_20240101_120000.db", "backup_20240102_120000.db", "backup_20231231_120000.db"}
	for _, f := range files {
		os.WriteFile(filepath.Join(dir, f), []byte("db"), 0o644)
	}
	db := NewDatabase(nil)
	backups, err := db.ListBackups(dir)
	if err != nil {
		t.Fatalf("ListBackups error: %v", err)
	}
	if len(backups) != 3 {
		t.Errorf("Expected 3 backups, got %d", len(backups))
	}
	if backups[0].name != "backup_20240102_120000.db" {
		t.Errorf("Expected first backup to be latest, got %s", backups[0].name)
	}
}

func TestValidateBackup_NonexistentFile(t *testing.T) {
	db := NewDatabase(nil)
	err := db.ValidateBackup("/does/not/exist.db")
	if err == nil {
		t.Error("Expected error for nonexistent file")
	}
}

func TestValidateBackup_ZeroSize(t *testing.T) {
	db := NewDatabase(nil)
	file := filepath.Join(t.TempDir(), "empty.db")
	os.WriteFile(file, []byte{}, 0o644)
	err := db.ValidateBackup(file)
	if err == nil || err.Error() != "backup file is empty" {
		t.Errorf("Expected 'backup file is empty', got %v", err)
	}
}

func setupTestDB(t *testing.T) (*Database, string, string) {
	// Create a temporary directory for test database and backups
	tmpDir := t.TempDir()

	// Create paths
	dbPath := filepath.Join(tmpDir, "test.db")
	backupDir := filepath.Join(tmpDir, "backups")

	// Create a simple SQLite database using the sqlite3 command with echo
	// This is a more reliable way to create a valid database file
	cmd := exec.Command("sqlite3", dbPath, "PRAGMA page_size=4096; PRAGMA user_version=1; CREATE TABLE test(id INTEGER PRIMARY KEY);")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "Failed to create test database: %s", string(output))

	// Verify the database was created and is valid
	validateCmd := exec.Command("sqlite3", dbPath, "PRAGMA integrity_check;")
	output, err = validateCmd.CombinedOutput()
	require.NoError(t, err, "Database validation failed: %s", string(output))
	require.Equal(t, "ok\n", string(output), "Database integrity check failed")

	log.Printf("Created valid test database at %s", dbPath)

	// Verify database was created
	info, err := os.Stat(dbPath)
	require.NoError(t, err, "Database file was not created")
	require.Greater(t, info.Size(), int64(0), "Database file is empty")

	// Initialize test Database instance with logger
	logger := logging.NewLogger(logging.Config{
		Level:   "info",
		Verbose: false,
		Quiet:   true,
	})

	return NewDatabase(logger), dbPath, backupDir
}

func TestBackupCreationAndRetention(t *testing.T) {
	db, dbPath, backupDir := setupTestDB(t)

	// Set shorter retention for testing
	db.SetRetentionConfig(RetentionConfig{
		DailyRetentionDays:   1,
		WeeklyRetentionDays:  2,
		MonthlyRetentionDays: 3,
	})

	// Create test backups with different dates and types
	testBackups := []struct {
		backupType BackupType
		age        time.Duration
		expected   bool // should it exist after cleanup?
	}{
		{Daily, 12 * time.Hour, true},    // recent daily
		{Daily, 36 * time.Hour, false},   // old daily
		{Weekly, 36 * time.Hour, true},   // recent weekly
		{Weekly, 72 * time.Hour, false},  // old weekly
		{Monthly, 48 * time.Hour, true},  // recent monthly
		{Monthly, 96 * time.Hour, false}, // old monthly
	}

	// Create test backup files
	now := time.Now()
	for _, tb := range testBackups {
		backupTime := now.Add(-tb.age)

		// Adjust the date based on backup type to ensure correct type detection
		switch tb.backupType {
		case Monthly:
			// Set to first day of month
			backupTime = time.Date(backupTime.Year(), backupTime.Month(), 1,
				backupTime.Hour(), backupTime.Minute(), backupTime.Second(), 0, backupTime.Location())
		case Weekly:
			// Set to Sunday
			daysUntilSunday := (7 - int(backupTime.Weekday())) % 7
			backupTime = backupTime.AddDate(0, 0, daysUntilSunday)
		}

		backupPath := filepath.Join(backupDir, "backup_"+backupTime.Format("20060102_150405")+".db")
		require.NoError(t, os.MkdirAll(filepath.Dir(backupPath), 0o755))

		file, err := os.Create(backupPath)
		require.NoError(t, err)
		_, err = file.Write([]byte("test data"))
		require.NoError(t, err)
		file.Close()

		// Set file modification time
		require.NoError(t, os.Chtimes(backupPath, backupTime, backupTime))
	}

	// Log the test database file info
	log.Printf("Test database at %s exists: %v", dbPath, fileExists(dbPath))

	// Add a small delay to ensure the database file from setupTestDB is fully released/flushed
	time.Sleep(1000 * time.Millisecond)

	// Create a new backup
	backupPath, err := db.BackupDatabase(dbPath, backupDir)
	assert.NoError(t, err)
	assert.NotEmpty(t, backupPath)

	// Check remaining backups
	backups, err := db.ListBackups(backupDir)
	assert.NoError(t, err)

	// Count remaining backups (should only have recent ones)
	expectedCount := 0
	for _, tb := range testBackups {
		if tb.expected {
			expectedCount++
		}
	}
	// +1 for the new backup we just created
	assert.Equal(t, expectedCount+1, len(backups), "Unexpected number of backups after cleanup")

	// Verify backup types
	for _, backup := range backups {
		assert.NotEmpty(t, backup.backupType, "Backup type should not be empty for %s", backup.name)
	}
}

// Helper function to check if a file exists
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

func TestRetentionConfigUpdate(t *testing.T) {
	db, _, _ := setupTestDB(t)

	// Test default config
	defaultConfig := db.GetRetentionConfig()
	assert.Equal(t, 7, defaultConfig.DailyRetentionDays)
	assert.Equal(t, 14, defaultConfig.WeeklyRetentionDays)
	assert.Equal(t, 90, defaultConfig.MonthlyRetentionDays)

	// Test config update
	newConfig := RetentionConfig{
		DailyRetentionDays:   3,
		WeeklyRetentionDays:  10,
		MonthlyRetentionDays: 60,
	}
	db.SetRetentionConfig(newConfig)

	updatedConfig := db.GetRetentionConfig()
	assert.Equal(t, newConfig, updatedConfig)
}
