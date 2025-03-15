package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"infinity-metrics-installer/internal/config"
	"infinity-metrics-installer/internal/logging"
)

// Option is a functional option for configuring Git Manager
type Option func(*Manager)

// Manager handles Git operations
type Manager struct {
	logger *logging.Logger
	config *config.Config
}

// WithLogger sets the logger for Git operations
func WithLogger(logger *logging.Logger) Option {
	return func(m *Manager) {
		m.logger = logger
	}
}

// NewManager creates a new Git manager
func NewManager(options ...Option) *Manager {
	// Create with default logger
	m := &Manager{
		logger: logging.NewLogger(logging.Config{Level: "info"}),
		config: config.NewConfig(),
	}

	// Apply options
	for _, option := range options {
		option(m)
	}

	return m
}

// runCommand executes a git command and returns its output and error
func (m *Manager) runCommand(dir string, args ...string) (string, error) {
	m.logger.Debug("Running git command: git %s", strings.Join(args, " "))

	var stdout, stderr bytes.Buffer
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

// SetupRepository clones or updates the installation repository
func (m *Manager) SetupRepository(installDir string) error {
	// Check if Git is installed
	if _, err := exec.LookPath("git"); err != nil {
		m.logger.Error("Git is not installed. Please install git before continuing.")
		return fmt.Errorf("git not installed: %w", err)
	}

	// deployment repository should be cloned inside the installation directory
	deploymentDirectory := filepath.Join(installDir, "deployment")
	repoURL := m.config.GetData().DeploymentRepoURL
	m.logger.Info("Setting up repository at %s", deploymentDirectory)

	// Check if deploymentDirectory exists
	if stat, err := os.Stat(deploymentDirectory); err == nil && stat.IsDir() {
		// Check if it’s a Git repository
		gitDir := filepath.Join(deploymentDirectory, ".git")
		if _, err := os.Stat(gitDir); err == nil {
			// Repository exists, update it
			m.logger.Info("Repository exists, updating")
			output, err := m.runCommand(deploymentDirectory, "pull")
			if err != nil {
				return fmt.Errorf("failed to update repository: %w", err)
			}

			if strings.Contains(output, "Already up to date") {
				m.logger.Info("Repository is already up to date")
			} else {
				m.logger.Success("Repository updated successfully")
				m.logger.Debug("Pull output: %s", output)
			}
		} else if os.IsNotExist(err) {
			// Directory exists but isn’t a Git repo
			dir, err := os.Open(deploymentDirectory)
			if err != nil {
				return fmt.Errorf("failed to open directory %s: %w", deploymentDirectory, err)
			}
			defer dir.Close()

			// Check if directory is non-empty
			if _, err := dir.Readdir(1); err == nil {
				// log directory contents
				dir, _ := os.Open(deploymentDirectory)
				files, _ := dir.Readdirnames(0)
				m.logger.Error("Directory %s is not empty: %v", deploymentDirectory, files)

				m.logger.Error("Directory %s exists and is non-empty but not a Git repository", deploymentDirectory)
				return fmt.Errorf("Infinity Metrics appears to be partially installed at %s; please remove the directory and retry", deploymentDirectory)
			}

			// Directory is empty, proceed with clone
			m.logger.Info("Repository not found, cloning from %s", repoURL)
			var stdout, stderr bytes.Buffer
			cmd := exec.Command("git", "clone", repoURL, deploymentDirectory)
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			if err := cmd.Run(); err != nil {
				return fmt.Errorf("failed to clone repository: %w - %s", err, stderr.String())
			}

			m.logger.Success("Repository cloned successfully")
			m.logger.Debug("Clone output: %s", stdout.String())
		}
	} else if os.IsNotExist(err) {
		// Directory doesn’t exist, clone it
		m.logger.Info("Repository not found, cloning from %s", repoURL)

		// Ensure parent directory exists
		parentDir := filepath.Dir(deploymentDirectory)
		if err := os.MkdirAll(parentDir, 0o755); err != nil {
			return fmt.Errorf("failed to create parent directory %s: %w", parentDir, err)
		}

		var stdout, stderr bytes.Buffer
		cmd := exec.Command("git", "clone", repoURL, deploymentDirectory)
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to clone repository: %w - %s", err, stderr.String())
		}

		m.logger.Success("Repository cloned successfully")
		m.logger.Debug("Clone output: %s", stdout.String())
	} else {
		return fmt.Errorf("failed to stat directory %s: %w", deploymentDirectory, err)
	}

	// Get repository information for logging
	m.logger.Debug("Getting repository information")

	// Get current branch
	branchOutput, err := m.runCommand(deploymentDirectory, "rev-parse", "--abbrev-ref", "HEAD")
	if err == nil {
		branch := strings.TrimSpace(branchOutput)
		m.logger.Debug("Current branch: %s", branch)
	}

	// Get latest commit
	commitOutput, err := m.runCommand(deploymentDirectory, "log", "-1", "--oneline")
	if err == nil {
		commit := strings.TrimSpace(commitOutput)
		m.logger.Debug("Latest commit: %s", commit)
	}

	m.logger.Success("Repository setup completed")
	return nil
}

// GetRepositoryInfo returns information about the repository
func (m *Manager) GetRepositoryInfo(installDir string) (map[string]string, error) {
	info := make(map[string]string)

	// Check if repository exists
	if _, err := os.Stat(filepath.Join(installDir, ".git")); os.IsNotExist(err) {
		return info, fmt.Errorf("repository not found at %s", installDir)
	}

	// Get current branch
	branchOutput, err := m.runCommand(installDir, "rev-parse", "--abbrev-ref", "HEAD")
	if err == nil {
		info["branch"] = strings.TrimSpace(branchOutput)
	}

	// Get latest commit hash
	hashOutput, err := m.runCommand(installDir, "rev-parse", "HEAD")
	if err == nil {
		info["commit"] = strings.TrimSpace(hashOutput)
	}

	// Get latest commit message
	messageOutput, err := m.runCommand(installDir, "log", "-1", "--pretty=%B")
	if err == nil {
		info["message"] = strings.TrimSpace(messageOutput)
	}

	// Get commit date
	dateOutput, err := m.runCommand(installDir, "log", "-1", "--format=%cd", "--date=iso")
	if err == nil {
		info["date"] = strings.TrimSpace(dateOutput)
	}

	// Get remote URL
	remoteOutput, err := m.runCommand(installDir, "config", "--get", "remote.origin.url")
	if err == nil {
		info["remote"] = strings.TrimSpace(remoteOutput)
	}

	return info, nil
}
