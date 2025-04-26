package database

import (
	"os"
	"path/filepath"
	"testing"
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
