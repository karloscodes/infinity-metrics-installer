package system

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"infinity-metrics-installer/internal/logging"
)

// Option is a functional option for configuring System
type Option func(*System)

// System handles system-level operations
type System struct {
	logger *logging.Logger
}

// WithLogger sets the logger for System operations
func WithLogger(logger *logging.Logger) Option {
	return func(s *System) {
		s.logger = logger
	}
}

// NewSystem creates a new System manager
func NewSystem(options ...Option) *System {
	// Create with default logger
	s := &System{
		logger: logging.NewLogger(logging.Config{Level: "info"}),
	}

	// Apply options
	for _, option := range options {
		option(s)
	}

	return s
}

// runCommand executes a command and returns its output and error
func (s *System) runCommand(name string, args ...string) (string, error) {
	s.logger.Debug("Running command: %s %s", name, strings.Join(args, " "))

	var stdout, stderr bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

// GetSystemInfo returns basic system information
func (s *System) GetSystemInfo() map[string]string {
	info := make(map[string]string)

	// Get OS information
	info["os"] = runtime.GOOS
	info["arch"] = runtime.GOARCH

	// Get hostname
	hostname, err := os.Hostname()
	if err == nil {
		info["hostname"] = hostname
	}

	// Get distribution info if on Linux
	if runtime.GOOS == "linux" {
		// Try to get release info
		if data, err := ioutil.ReadFile("/etc/os-release"); err == nil {
			lines := strings.Split(string(data), "\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "PRETTY_NAME=") {
					distro := strings.TrimPrefix(line, "PRETTY_NAME=")
					distro = strings.Trim(distro, "\"")
					info["distribution"] = distro
					break
				}
			}
		}
	}

	return info
}

// InstallDependencies installs required system dependencies
func (s *System) InstallDependencies() error {
	// Check the operating system
	if runtime.GOOS != "linux" {
		s.logger.Warn("Non-Linux OS detected: %s. Dependency installation may not work correctly.", runtime.GOOS)
	}

	s.logger.Info("Installing required dependencies")

	// Check if we're on a Debian/Ubuntu system
	_, err := os.Stat("/etc/debian_version")
	isDebianBased := err == nil

	if isDebianBased {
		// Debian/Ubuntu approach
		s.logger.Info("Updating package lists")
		output, err := s.runCommand("apt-get", "update")
		if err != nil {
			return fmt.Errorf("failed to update package lists: %w", err)
		}
		s.logger.Debug("apt-get update output: %s", output)

		s.logger.Info("Installing git and curl")
		output, err = s.runCommand("apt-get", "install", "-y", "git", "curl")
		if err != nil {
			return fmt.Errorf("failed to install dependencies: %w", err)
		}
		s.logger.Debug("apt-get install output: %s", output)
	} else {
		// Try to detect other package managers
		packageManagers := []struct {
			cmd     string
			update  []string
			install []string
		}{
			{"dnf", []string{"check-update"}, []string{"install", "-y", "git", "curl"}},
			{"yum", []string{"check-update"}, []string{"install", "-y", "git", "curl"}},
			{"zypper", []string{"refresh"}, []string{"install", "-y", "git", "curl"}},
			{"pacman", []string{"-Sy"}, []string{"-S", "--noconfirm", "git", "curl"}},
		}

		for _, pm := range packageManagers {
			if _, err := exec.LookPath(pm.cmd); err == nil {
				// Found a package manager
				s.logger.Info("Using %s package manager", pm.cmd)

				// Update package lists
				s.logger.Info("Updating package lists")
				if _, err := s.runCommand(pm.cmd, pm.update...); err != nil {
					s.logger.Warn("Failed to update package lists: %s", err)
					// Continue anyway
				}

				// Install packages
				s.logger.Info("Installing git and curl")
				if output, err := s.runCommand(pm.cmd, pm.install...); err != nil {
					return fmt.Errorf("failed to install dependencies: %w", err)
				} else {
					s.logger.Debug("%s install output: %s", pm.cmd, output)
				}

				s.logger.Success("Dependencies installed successfully")
				return nil
			}
		}

		// If we get here, we couldn't find a package manager
		s.logger.Warn("Could not detect package manager. Dependencies may not be installed correctly.")
		s.logger.Warn("Please ensure git and curl are installed manually.")
	}

	// Verify installations
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git installation could not be verified: %w", err)
	}

	if _, err := exec.LookPath("curl"); err != nil {
		return fmt.Errorf("curl installation could not be verified: %w", err)
	}

	s.logger.Success("Dependencies installed successfully")
	return nil
}

// CreateInstallDir creates the installation directory
func (s *System) CreateInstallDir(installDir string) error {
	s.logger.Info("Creating installation directory: %s", installDir)

	// Check if directory already exists
	if _, err := os.Stat(installDir); err == nil {
		s.logger.Info("Installation directory already exists")
	} else {
		// Create directory
		if err := os.MkdirAll(installDir, 0o755); err != nil {
			return fmt.Errorf("failed to create installation directory: %w", err)
		}
		s.logger.Success("Installation directory created")
	}

	// Check if we have write permissions
	testFile := filepath.Join(installDir, ".write-test")
	if err := os.WriteFile(testFile, []byte("test"), 0o644); err != nil {
		return fmt.Errorf("no write permission in installation directory: %w", err)
	}
	os.Remove(testFile)

	return nil
}

// SetupCronJob sets up the cron job for automatic updates
func (s *System) SetupCronJob(installDir string) error {
	s.logger.Info("Setting up automatic updates")

	// Use the updater binary instead of a script
	updaterBinary := filepath.Join(installDir, "infinity-metrics-updater")
	if _, err := os.Stat(updaterBinary); err != nil {
		return fmt.Errorf("updater binary not found at %s: %w", updaterBinary, err)
	}

	s.logger.Debug("Making updater binary executable")
	if err := os.Chmod(updaterBinary, 0o755); err != nil {
		return fmt.Errorf("failed to make updater binary executable: %w", err)
	}

	// Create the cron job using the binary
	cronJob := fmt.Sprintf("0 3 * * * cd %s && ./infinity-metrics-updater >> %s/logs/updater.log 2>&1",
		installDir, installDir)

	s.logger.Debug("Cron job will be: %s", cronJob)

	// Get existing crontab
	var existingCron []byte
	getCronCmd := exec.Command("crontab", "-l")
	var stdout, stderr bytes.Buffer
	getCronCmd.Stdout = &stdout
	getCronCmd.Stderr = &stderr
	err := getCronCmd.Run()

	// Only use existing crontab if the command succeeded
	if err == nil {
		existingCron = stdout.Bytes()
		s.logger.Debug("Retrieved existing crontab")
	} else {
		s.logger.Debug("No existing crontab found or error retrieving it: %v", err)
	}

	// Create a temporary file with new crontab
	tmpFile := filepath.Join(os.TempDir(), "infinity-crontab")
	s.logger.Debug("Creating temporary crontab file: %s", tmpFile)

	file, err := os.Create(tmpFile)
	if err != nil {
		return fmt.Errorf("failed to create temporary crontab file: %w", err)
	}
	defer os.Remove(tmpFile) // Ensure cleanup even on error

	// Write existing crontab to file, filtering out any existing updater jobs
	if len(existingCron) > 0 {
		lines := string(existingCron)
		for _, line := range strings.Split(lines, "\n") {
			if !strings.Contains(line, "infinity-metrics-updater") && line != "" {
				fmt.Fprintln(file, line)
			}
		}
	}

	// Add our new cron job
	fmt.Fprintln(file, cronJob)
	file.Close()

	// Install new crontab
	s.logger.Info("Installing new crontab")
	installCronCmd := exec.Command("crontab", tmpFile)

	var installStdout, installStderr bytes.Buffer
	installCronCmd.Stdout = &installStdout
	installCronCmd.Stderr = &installStderr

	if err := installCronCmd.Run(); err != nil {
		return fmt.Errorf("failed to install crontab: %w - %s", err, installStderr.String())
	}

	s.logger.Success("Automatic updates scheduled for 3:00 AM daily")
	return nil
}

// CheckDiskSpace checks if there's enough disk space in the installation directory
func (s *System) CheckDiskSpace(installDir string, requiredMB int) error {
	s.logger.Info("Checking disk space")

	// Ensure the directory exists
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory for disk check: %w", err)
	}

	// On Linux and macOS, use df
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		output, err := s.runCommand("df", "-m", installDir)
		if err != nil {
			return fmt.Errorf("failed to check disk space: %w", err)
		}

		// Parse output - typically the 4th column is available space in MB
		lines := strings.Split(output, "\n")
		if len(lines) >= 2 {
			fields := strings.Fields(lines[1])
			if len(fields) >= 4 {
				var availableMB int
				fmt.Sscanf(fields[3], "%d", &availableMB)

				s.logger.Debug("Available disk space: %d MB", availableMB)

				if availableMB < requiredMB {
					return fmt.Errorf("insufficient disk space: %d MB available, %d MB required",
						availableMB, requiredMB)
				}

				s.logger.Success("Sufficient disk space available")
				return nil
			}
		}

		return fmt.Errorf("failed to parse disk space output")
	}

	// For other OS, just warn
	s.logger.Warn("Disk space check not implemented for %s", runtime.GOOS)
	return nil
}
