package main

import (
	"bufio"
	"flag"
	"fmt"
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
	startTime := time.Now()

	logLevel := flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	verbose := flag.Bool("verbose", false, "Enable verbose output")
	quiet := flag.Bool("quiet", false, "Suppress non-error output")
	version := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	// Configure the main logger to log to stdout
	logger := logging.NewLogger(logging.Config{
		Level:   *logLevel,
		Verbose: *verbose,
		Quiet:   *quiet,
	})

	if *version {
		fmt.Printf("Infinity Metrics Installer v%s\n", currentInstallerVersion)
		os.Exit(0)
	}

	inst := installer.NewInstaller(logger)

	if len(os.Args) < 2 {
		printUsage(logger)
		os.Exit(2)
	}

	command := os.Args[1]
	fmt.Printf("Infinity Metrics Installer v%s\n", currentInstallerVersion)

	switch command {
	case "install":
		logger.Debug("Starting 'install' command")
		runInstall(inst, logger, startTime)
	case "update":
		logger.Debug("Starting 'update' command")
		runUpdate(inst, logger, startTime)
	case "restore":
		logger.Debug("Starting 'restore' command")
		runRestore(inst, logger, startTime)
	case "change-admin-password":
		logger.Debug("Starting 'change-admin-password' command")
		runChangeAdminPassword(logger, startTime)
	case "create-admin-user":
		logger.Debug("Starting 'create-admin-user' command")
		runCreateAdminUser(logger, startTime)
	case "help":
		logger.Debug("Starting 'help' command")
		printUsage(logger)
		os.Exit(0)
	default:
		logger.Error("Invalid command: %s", command)
		printUsage(logger)
		os.Exit(2)
	}
}

func runInstall(inst *installer.Installer, logger *logging.Logger, startTime time.Time) {
	fmt.Println("Starting Infinity Metrics Installation")
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
	os.Stdout.Sync() // Force flush to ensure output is captured
}

func runUpdate(inst *installer.Installer, logger *logging.Logger, startTime time.Time) {
	fmt.Println("Starting Infinity Metrics Update")
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
	fmt.Println("Starting Infinity Metrics Restore")

	logger.Info("Running restore...")
	err := inst.Restore()
	if err != nil {
		logger.Error("Restore failed: %v", err)
		os.Exit(1)
	}

	elapsedTime := time.Since(startTime).Round(time.Second)
	logger.Success("Restore completed in %s", elapsedTime)
}

func runChangeAdminPassword(logger *logging.Logger, startTime time.Time) {
	adminMgr := admin.NewManager(logger)
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Enter admin email: ")
	emailInput, err := reader.ReadString('\n')
	if err != nil {
		logger.Error("Failed to read email: %v", err)
		os.Exit(1)
	}
	email := strings.TrimSpace(emailInput)
	if email == "" {
		logger.Error("Email cannot be empty")
		os.Exit(1)
	}

	var password string
	for {
		fmt.Print("Enter new admin password (minimum 8 characters): ")
		passBytes, err := term.ReadPassword(int(syscall.Stdin))
		if err != nil {
			logger.Error("Failed to read password: %v", err)
			os.Exit(1)
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
			os.Exit(1)
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
		os.Exit(1)
	}

	elapsed := time.Since(startTime).Round(time.Second)
	logger.Success("Password changed in %s", elapsed)
}

func runCreateAdminUser(logger *logging.Logger, startTime time.Time) {
	adminMgr := admin.NewManager(logger)
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Enter admin email: ")
	emailInput, err := reader.ReadString('\n')
	if err != nil {
		logger.Error("Failed to read email: %v", err)
		os.Exit(1)
	}
	email := strings.TrimSpace(emailInput)
	if email == "" {
		logger.Error("Email cannot be empty")
		os.Exit(1)
	}

	var password string
	for {
		fmt.Print("Enter admin password (minimum 8 characters): ")
		passBytes, err := term.ReadPassword(int(syscall.Stdin))
		if err != nil {
			logger.Error("Failed to read password: %v", err)
			os.Exit(1)
		}
		fmt.Println()

		password = strings.TrimSpace(string(passBytes))
		if len(password) < 8 {
			fmt.Println("Error: Password must be at least 8 characters long.")
			continue
		}

		fmt.Print("Confirm admin password: ")
		confirmBytes, err := term.ReadPassword(int(syscall.Stdin))
		if err != nil {
			logger.Error("Failed to read confirmation password: %v", err)
			os.Exit(1)
		}
		fmt.Println()

		confirm := strings.TrimSpace(string(confirmBytes))
		if password != confirm {
			fmt.Println("Error: Passwords do not match. Please try again.")
			continue
		}
		break
	}

	if err := adminMgr.CreateAdminUser(email, password); err != nil {
		logger.Error("Failed to create admin user: %v", err)
		os.Exit(1)
	}

	elapsed := time.Since(startTime).Round(time.Second)
	logger.Success("Admin user created in %s", elapsed)
}

func printUsage(logger *logging.Logger) {
	fmt.Println("------------------------------------------------------------------")
	fmt.Printf("  Infinity Metrics Installer v%s - https://getinfinitymetrics.com\n", currentInstallerVersion)
	fmt.Println("------------------------------------------------------------------")
	fmt.Println("\nUsage:")
	fmt.Println("  infinity-metrics-installer [command]")
	fmt.Println("\nCommands:")
	fmt.Println("  install  Install Infinity Metrics")
	fmt.Println("  update   Update Infinity Metrics")
	fmt.Println("  restore  Restore the last database backup")
	fmt.Println("  create-admin-user     Create an admin user inside the running container")
	fmt.Println("  change-admin-password  Change the admin user's password")
	fmt.Println("  help     Show this help message")
	fmt.Println("\nFlags:")
	flag.PrintDefaults()
	fmt.Println("------------------------------------------------------------------")
}
