package database

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"infinity-metrics-installer/internal/logging"
)

// BackupType represents the type of backup (daily, weekly, monthly)
type BackupType string

const (
	Daily   BackupType = "daily"
	Weekly  BackupType = "weekly"
	Monthly BackupType = "monthly"
)

// BackupFile represents a database backup file
type BackupFile struct {
	name     string
	path     string
	backupType BackupType
	createdAt time.Time
}

// RetentionConfig defines the retention period for each backup type
type RetentionConfig struct {
	DailyRetentionDays   int
	WeeklyRetentionDays  int
	MonthlyRetentionDays int
}

// DefaultRetentionConfig provides default retention values
func DefaultRetentionConfig() RetentionConfig {
	return RetentionConfig{
		DailyRetentionDays:   7,  // Keep daily backups for 7 days
		WeeklyRetentionDays:  14, // Keep weekly backups for 2 weeks
		MonthlyRetentionDays: 90, // Keep monthly backups for 3 months
	}
}

// Database manages database operations
type Database struct {
	logger    *logging.Logger
	retention RetentionConfig
}

// NewDatabase creates a new Database instance
func NewDatabase(logger *logging.Logger) *Database {
	return &Database{
		logger:    logger,
		retention: DefaultRetentionConfig(),
	}
}

// EnsureSQLiteInstalled installs SQLite if not already available
func (d *Database) EnsureSQLiteInstalled() error {
	d.logger.Info("Checking for SQLite installation...")

	// Try to run sqlite3 --version to check if it's installed
	cmd := exec.Command("sqlite3", "--version")
	if err := cmd.Run(); err == nil {
		d.logger.Success("SQLite is already installed")
		return nil
	}

	// SQLite is not installed, install it using apt-get (assuming Debian/Ubuntu)
	d.logger.Info("Installing SQLite...")
	if os.Geteuid() != 0 {
		return fmt.Errorf("must run as root to install SQLite")
	}

	// Install sqlite3
	installCmd := exec.Command("apt-get", "install", "-y", "sqlite3")
	if err := installCmd.Run(); err != nil {
		return fmt.Errorf("failed to install SQLite: %w", err)
	}

	// Verify installation
	verifyCmd := exec.Command("sqlite3", "--version")
	if err := verifyCmd.Run(); err != nil {
		return fmt.Errorf("SQLite installation verification failed: %w", err)
	}

	return nil
}

// determineBackupType determines the type of backup based on its creation time
func determineBackupType(createdAt time.Time) BackupType {
	// If it's the first day of the month, it's a monthly backup
	if createdAt.Day() == 1 {
		return Monthly
	}
	// If it's Sunday, it's a weekly backup
	if createdAt.Weekday() == time.Sunday {
		return Weekly
	}
	// Otherwise, it's a daily backup
	return Daily
}

// cleanupOldBackups removes old backups according to retention policy
// SetRetentionConfig updates the retention configuration
func (d *Database) SetRetentionConfig(config RetentionConfig) {
	d.retention = config
	d.logger.Info("Updated backup retention config: daily=%d days, weekly=%d days, monthly=%d days",
		config.DailyRetentionDays, config.WeeklyRetentionDays, config.MonthlyRetentionDays)
}

// GetRetentionConfig returns the current retention configuration
func (d *Database) GetRetentionConfig() RetentionConfig {
	return d.retention
}

func (d *Database) cleanupOldBackups(backupDir string) error {
	backups, err := d.ListBackups(backupDir)
	if err != nil {
		return fmt.Errorf("failed to list backups: %w", err)
	}

	// Convert retention days to durations
	dailyRetention := time.Duration(d.retention.DailyRetentionDays) * 24 * time.Hour
	weeklyRetention := time.Duration(d.retention.WeeklyRetentionDays) * 24 * time.Hour
	monthlyRetention := time.Duration(d.retention.MonthlyRetentionDays) * 24 * time.Hour

	now := time.Now()
	for _, backup := range backups {
		age := now.Sub(backup.createdAt)

		shouldDelete := false
		switch backup.backupType {
		case Daily:
			shouldDelete = age > dailyRetention
		case Weekly:
			shouldDelete = age > weeklyRetention
		case Monthly:
			shouldDelete = age > monthlyRetention
		}

		if shouldDelete {
			d.logger.Info("Removing old %s backup: %s (age: %v)", backup.backupType, backup.name, age.Round(time.Hour))
			if err := os.Remove(backup.path); err != nil {
				d.logger.Warn("Failed to remove old backup %s: %v", backup.name, err)
			}
		}
	}

	return nil
}

// BackupDatabase creates a backup of the SQLite database using sqlite3
func (d *Database) BackupDatabase(dbPath, backupDir string) (string, error) {
	// Check if the database file exists
	if _, err := os.Stat(dbPath); err != nil {
		return "", fmt.Errorf("database file not found: %w", err)
	}

	// Ensure backup directory exists
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Generate a timestamped backup filename
	timestamp := time.Now().Format("20060102_150405")
	backupFile := filepath.Join(backupDir, fmt.Sprintf("backup_%s.db", timestamp))

	d.logger.Info("Creating backup of %s", dbPath)

	// Create backup using SQLite's .backup command
	cmd := exec.Command("sqlite3", dbPath, fmt.Sprintf(".backup '%s'", backupFile))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("sqlite3 backup failed: %w - %s", err, stderr.String())
	}

	// Verify the backup was created
	backupInfo, err := os.Stat(backupFile)
	if err != nil {
		return "", fmt.Errorf("failed to create backup: %w", err)
	}

	// Verify the backup has content
	if backupInfo.Size() == 0 {
		os.Remove(backupFile) // Clean up empty backup
		return "", fmt.Errorf("backup file is empty")
	}

	// Validate the backup
	if err := d.ValidateBackup(backupFile); err != nil {
		os.Remove(backupFile) // Clean up invalid backup
		return "", fmt.Errorf("backup validation failed: %w", err)
	}

	d.logger.Success("Database backup created at %s (size: %d bytes)", backupFile, backupInfo.Size())

	// Clean up old backups according to retention policy
	if err := d.cleanupOldBackups(backupDir); err != nil {
		d.logger.Warn("Failed to clean up old backups: %v", err)
	}

	return backupFile, nil
}

// ListBackups scans the backup directory and returns a sorted list of backup files
func (d *Database) ListBackups(backupDir string) ([]BackupFile, error) {
	files, err := os.ReadDir(backupDir)
	if err != nil {
		return nil, err
	}

	var backups []BackupFile
	for _, file := range files {
		if !file.IsDir() && strings.HasPrefix(file.Name(), "backup_") && strings.HasSuffix(file.Name(), ".db") {
			// Parse timestamp from filename (format: backup_20060102_150405.db)
			timePart := strings.TrimPrefix(strings.TrimSuffix(file.Name(), ".db"), "backup_")
			createdAt, err := time.Parse("20060102_150405", timePart)
			if err != nil {
				d.logger.Warn("Skipping backup with invalid timestamp: %s", file.Name())
				continue
			}

			// Determine backup type
			backupType := determineBackupType(createdAt)

			backups = append(backups, BackupFile{
				name:      file.Name(),
				path:      filepath.Join(backupDir, file.Name()),
				backupType: backupType,
				createdAt: createdAt,
			})
		}
	}

	// Sort by creation time descending
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].createdAt.After(backups[j].createdAt)
	})

	return backups, nil
}

// PromptSelection displays backups and prompts the user to select one
func (d *Database) PromptSelection(backups []BackupFile) (string, error) {
	if len(backups) == 0 {
		return "", fmt.Errorf("no backups available")
	}

	d.logger.Info("Available backups:")
	for i, backup := range backups {
		info, _ := os.Stat(backup.path)
		d.logger.Info("%d: %s (size: %d bytes, modified: %s)", i+1, backup.name, info.Size(), info.ModTime().Format(time.RFC1123))
	}

	reader := bufio.NewReader(os.Stdin)
	d.logger.Info("Enter the number of the backup to restore (1-%d): ", len(backups))
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	choice, err := strconv.Atoi(strings.TrimSpace(input))
	if err != nil || choice < 1 || choice > len(backups) {
		return "", fmt.Errorf("invalid selection: must be a number between 1 and %d", len(backups))
	}

	return backups[choice-1].path, nil
}

// ValidateBackup checks if a backup file is valid and not corrupted
func (d *Database) ValidateBackup(backupFile string) error {
	stat, err := os.Stat(backupFile)
	if err != nil {
		return fmt.Errorf("cannot access backup: %w", err)
	}
	if stat.Size() == 0 {
		return fmt.Errorf("backup file is empty")
	}

	// SQLite integrity check using PRAGMA integrity_check
	cmd := exec.Command("sqlite3", backupFile, "PRAGMA integrity_check;")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		d.logger.Warn("SQLite integrity check failed: %s", stderr.String())
		return fmt.Errorf("backup may be corrupted: %w", err)
	}

	// Check the output - it should be "ok" for a valid database
	output := strings.TrimSpace(stdout.String())
	if output != "ok" {
		d.logger.Warn("SQLite integrity check returned issues: %s", output)
		return fmt.Errorf("backup integrity issues detected")
	}

	d.logger.Debug("Backup file %s validated successfully", backupFile)
	return nil
}

// RestoreDatabase restores a backup to the main database path
func (d *Database) RestoreDatabase(mainDBPath, backupPath string) error {
	// Validate the backup
	if err := d.ValidateBackup(backupPath); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Backup current DB (safety net)
	currentBackup := mainDBPath + ".bak." + time.Now().Format("20060102150405")
	if _, err := os.Stat(mainDBPath); err == nil {
		d.logger.Info("Backing up current database to %s", currentBackup)
		if err := os.Rename(mainDBPath, currentBackup); err != nil {
			return fmt.Errorf("backup current DB: %w", err)
		}
	}

	// Restore selected backup
	d.logger.Info("Restoring %s to %s", backupPath, mainDBPath)
	if err := os.Rename(backupPath, mainDBPath); err != nil {
		// Attempt rollback
		if err2 := os.Rename(currentBackup, mainDBPath); err2 != nil {
			d.logger.Error("Rollback failed: %v", err2)
		}
		return fmt.Errorf("restore backup: %w", err)
	}

	d.logger.Info("Database restored successfully from %s", backupPath)
	return nil
}
