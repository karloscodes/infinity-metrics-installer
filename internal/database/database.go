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

// BackupFile represents a database backup file
type BackupFile struct {
	name string
	path string
}

// Database manages database operations
type Database struct {
	logger *logging.Logger
}

// NewDatabase creates a new Database instance
func NewDatabase(logger *logging.Logger) *Database {
	return &Database{
		logger: logger,
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

	d.logger.Success("SQLite installed successfully")
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
			backups = append(backups, BackupFile{
				name: file.Name(),
				path: filepath.Join(backupDir, file.Name()),
			})
		}
	}

	// Sort by name (timestamp) descending
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].name > backups[j].name
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
func (d *Database) ValidateBackup(backupPath string) error {
	stat, err := os.Stat(backupPath)
	if err != nil {
		return fmt.Errorf("cannot access backup: %w", err)
	}
	if stat.Size() == 0 {
		return fmt.Errorf("backup file is empty")
	}

	// SQLite integrity check
	cmd := exec.Command("sqlite3", backupPath, ".dbinfo")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		d.logger.Warn("SQLite integrity check failed: %s", stderr.String())
		return fmt.Errorf("backup may be corrupted: %w", err)
	}

	d.logger.Debug("Backup file %s validated successfully", backupPath)
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
