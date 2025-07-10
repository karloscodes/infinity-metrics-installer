package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"infinity-metrics-installer/internal/admin"
	"infinity-metrics-installer/internal/config"
	"infinity-metrics-installer/internal/installer"
	"infinity-metrics-installer/internal/logging"
	"infinity-metrics-installer/internal/updater"
)

var currentInstallerVersion string = "dev"

func main() {
	// Detect the current working directory
	workingDirectory, err := os.Getwd()
	if err != nil {
		fmt.Printf("Error: Failed to determine working directory: %v\n", err)
		os.Exit(1)
	}

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	// Initialize logging
	startTime := time.Now()
	logger := initLogging()
	logger.Debug("Installer version: %s", currentInstallerVersion)
	logger.Debug("Working directory: %s", workingDirectory)

	inst := installer.NewInstaller(logger)

	// Update environment variables with current version
	os.Setenv("INFINITY_METRICS_VERSION", currentInstallerVersion)

	switch os.Args[1] {
	case "install":
		runInstall(inst, logger, startTime)
	case "update":
		runUpdate(inst, logger, startTime)
	case "reload":
		runReload(logger, startTime)
	case "restore":
		runRestore(inst, logger, startTime)
	case "change-admin-password":
		if err := runAdminPasswordChange(logger); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Printf("Unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func initLogging() *logging.Logger {
	logLevel := "info"
	if envLevel := os.Getenv("LOG_LEVEL"); envLevel != "" {
		logLevel = envLevel
	}

	verbose := false
	if os.Getenv("VERBOSE") == "true" {
		verbose = true
	}

	quiet := false
	if os.Getenv("QUIET") == "true" {
		quiet = true
	}

	// Configure the main logger to log to stdout
	logger := logging.NewLogger(logging.Config{
		Level:   logLevel,
		Verbose: verbose,
		Quiet:   quiet,
	})

	return logger
}

func runInstall(inst *installer.Installer, logger *logging.Logger, startTime time.Time) {
	logger.Debug("Initializing installation environment")

	// Create a bufio.Reader to read user input from stdin
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Please provide the required configuration details:")
	config := config.NewConfig(logger)
	if err := config.CollectFromUser(reader); err != nil {
		logger.Error("Failed to collect configuration: %v", err)
		os.Exit(1)
	}

	logger.Info("Running installation...")
	err := inst.RunWithConfig(config)
	if err != nil {
		logger.Error("Installation failed: %v", err)
		os.Exit(1)
	}

	elapsedTime := time.Since(startTime).Round(time.Second)
	logger.Success("Installation completed in %s", elapsedTime)

	data := inst.GetConfig().GetData()
	logger.InfoWithTime("Access your dashboard at https://%s", data.Domain)
	logger.Info("Login with your admin email: %s", data.AdminEmail)
	logger.Info("First-time login steps:")
	logger.Info("1. Confirm your organization name")
	logger.Info("2. Set your time zone")
	logger.Info("3. Choose your initial data sources to connect")

	// Check ports 80 and 443
	portCheck80 := checkPort(80)
	portCheck443 := checkPort(443)

	if !portCheck80 || !portCheck443 {
		logger.Warn("Important: One or more required ports are not available:")
		if !portCheck80 {
			logger.Warn("- Port 80 is not available (required for HTTP)")
		}
		if !portCheck443 {
			logger.Warn("- Port 443 is not available (required for HTTPS)")
		}
		logger.Warn("This may prevent SSL certificate generation and web access to your installation.")
		logger.Warn("Please ensure these ports are open in your firewall and not used by other services.")
	}

	os.Stdout.Sync() // Force flush to ensure output is captured
}

// checkPort checks if a port is available
func checkPort(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

func runUpdate(inst *installer.Installer, logger *logging.Logger, startTime time.Time) {
	logger.Debug("Initializing update environment")

	updater := updater.NewUpdater(logger)
	logger.Info("Running update...")
	err := updater.Run(currentInstallerVersion)
	if err != nil {
		logger.Error("Update failed: %v", err)
		os.Exit(1)
	}

	elapsedTime := time.Since(startTime).Round(time.Second)
	logger.Success("Update completed in %s", elapsedTime)
}

func runRestore(inst *installer.Installer, logger *logging.Logger, startTime time.Time) {
	logger.Info("Running restore...")
	err := inst.Restore()
	if err != nil {
		logger.Error("Restore failed: %v", err)
		os.Exit(1)
	}

	elapsedTime := time.Since(startTime).Round(time.Second)
	logger.Success("Restore completed in %s", elapsedTime)
}

func runReload(logger *logging.Logger, startTime time.Time) {
	fmt.Println("Reloading containers with latest configuration")
	logger.Debug("Initializing reload environment")

	reloader := updater.NewReloader(logger)
	logger.Info("Reloading containers...")
	err := reloader.Run()
	if err != nil {
		logger.Error("Reload failed: %v", err)
		os.Exit(1)
	}

	elapsedTime := time.Since(startTime).Round(time.Second)
	logger.Success("Reload completed in %s", elapsedTime)
}

func runAdminPasswordChange(logger *logging.Logger) error {
	adminMgr := admin.NewManager(logger)
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Enter admin email: ")
	emailInput, err := reader.ReadString('\n')
	if err != nil {
		logger.Error("Failed to read email: %v", err)
		return err
	}
	email := strings.TrimSpace(emailInput)
	if email == "" {
		logger.Error("Email cannot be empty")
		return nil
	}

	var password string
	for {
		fmt.Print("Enter new admin password (minimum 8 characters): ")
		passBytes, err := term.ReadPassword(int(syscall.Stdin))
		if err != nil {
			logger.Error("Failed to read password: %v", err)
			return err
		}
		fmt.Println()

		password = strings.TrimSpace(string(passBytes))
		if len(password) < 8 {
			fmt.Println("Error: Password must be at least 8 characters long.")
			continue
		}

		fmt.Print("Confirm new admin password: ")
		confirmBytes, err := term.ReadPassword(int(syscall.Stdin))
		if err != nil {
			logger.Error("Failed to read confirmation password: %v", err)
			return err
		}
		fmt.Println()

		confirm := strings.TrimSpace(string(confirmBytes))
		if password != confirm {
			fmt.Println("Error: Passwords do not match. Please try again.")
			continue
		}
		break
	}

	if err := adminMgr.ChangeAdminPassword(email, password); err != nil {
		logger.Error("Failed to change admin password: %v", err)
		return err
	}

	elapsed := time.Since(time.Now()).Round(time.Second)
	logger.Success("Password changed in %s", elapsed)
	return nil
}

func printUsage() {
	fmt.Println("Usage: infinity-metrics [command]")
	fmt.Println("\nCommands:")
	fmt.Println("  install                     Install Infinity Metrics")
	fmt.Println("  update                      Update an existing installation")
	fmt.Println("  reload                      Reload containers with latest .env config without backup")
	fmt.Println("  restore                     Restore the database from last backup")
	fmt.Println("  change-admin-password       Change the admin user password")
	fmt.Println("  help                        Show this help message")
}
