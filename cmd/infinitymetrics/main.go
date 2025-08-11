package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"infinity-metrics-installer/internal/admin"
	"infinity-metrics-installer/internal/config"
	"infinity-metrics-installer/internal/errors"
	"infinity-metrics-installer/internal/installer"
	"infinity-metrics-installer/internal/logging"
	"infinity-metrics-installer/internal/requirements"
	"infinity-metrics-installer/internal/updater"
	"infinity-metrics-installer/internal/validation"
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
	case "restore-db":
		runRestoreDB(inst, logger, startTime)
	case "change-admin-password":
		if err := runAdminPasswordChange(logger); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
	case "version", "--version", "-v":
		printVersion()
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

	// Welcome message with DNS information
	fmt.Println("🚀 Welcome to Infinity Metrics Installer!")
	fmt.Println()
	fmt.Println("📋 System Requirements:")
	fmt.Println("   • Ports 80 and 443 must be available (required for HTTP/HTTPS and SSL)")
	fmt.Println("   • Root privileges (run with sudo)")
	fmt.Println("   • Internet connection for downloading components")
	fmt.Println()
	fmt.Println("📋 DNS Configuration (Optional but Recommended):")
	fmt.Println("   • If you set up A/AAAA DNS records for your domain BEFORE installation,")
	fmt.Println("     the installer will automatically configure SSL certificates.")
	fmt.Println("   • You can also configure DNS records later, but SSL setup won't be immediate.")
	fmt.Println("   • The system will work either way - SSL will be configured automatically")
	fmt.Println("     once DNS propagation is complete.")
	fmt.Println()
	fmt.Println("🔒 SSL Certificate Information:")
	fmt.Println("   • SSL certificates are provided by Let's Encrypt with automatic renewal")
	fmt.Println("   • If SSL setup fails initially, the system will automatically retry, adding some delays.")
	fmt.Println("   • Let's Encrypt has rate limits to prevent abuse (see: https://letsencrypt.org/docs/rate-limits/)")
	fmt.Println()

	// System requirements check
	checker := requirements.NewChecker(logger)
	if err := checker.CheckSystemRequirements(); err != nil {
		logger.Error("System requirements check failed: %v", err)
		os.Exit(1)
	}

	fmt.Println("Please provide the required configuration details:")

	// Create a bufio.Reader to read user input from stdin
	reader := bufio.NewReader(os.Stdin)

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

	// Verify the installation and check for warnings
	logger.Info("Verifying installation...")
	_, verifyErr := inst.VerifyInstallation()

	if verifyErr != nil {
		logger.Warn("Installation verification had issues: %v", verifyErr)
		logger.Info("You may need to troubleshoot these issues before using Infinity Metrics")
	} else {
		logger.Success("Installation verified successfully")
	}

	// DNS warnings (if any)
	if inst.GetConfig().HasDNSWarnings() {
		// Only print the DNS CONFIGURATION REQUIRED block here (summary), not earlier in the process
		fmt.Println("\n\033[1m⚠️  DNS CONFIGURATION REQUIRED\033[0m")
		fmt.Println(strings.Repeat("-", 40))
		fmt.Println("The following DNS issues were detected during installation:")
		for _, warning := range config.GetDNSWarnings() {
			if strings.HasPrefix(warning, "Suggestion:") {
				fmt.Printf("   💡 %s\n", warning[11:])
			} else {
				fmt.Printf("   • %s\n", warning)
			}
		}
		fmt.Println("\n🛠️  NEXT STEPS:")
		data := inst.GetConfig().GetData()
		fmt.Printf("   1. Configure DNS: Add A/AAAA record for %s pointing to this server\n", data.Domain)
		fmt.Println("   2. Wait for DNS propagation (up to 24 hours)")
		fmt.Printf("   3. Test access: https://%s\n", data.Domain)
		fmt.Println("   4. Monitor logs: sudo tail -f /opt/infinity-metrics/logs/caddy.log")
		fmt.Println("\n📋 Note: All components are installed. The system will work once DNS is configured.")
		fmt.Println("📋 SSL setup might not be immediate due to Let's Encrypt retries.")
	}

	elapsedTime := time.Since(startTime).Round(time.Second)
	logger.Success("Installation completed in %s", elapsedTime)

	// Final success message with dashboard access information
	fmt.Println()
	fmt.Println("🎉 Installation Complete!")
	fmt.Println("═══════════════════════════")
	data := inst.GetConfig().GetData()
	fmt.Printf("🌐 Dashboard URL: https://%s\n", data.Domain)
	fmt.Printf("📧 Admin Email: %s\n", data.AdminEmail)
	fmt.Printf("🔑 Use the password you set during installation to log in\n")
	fmt.Println()
	fmt.Println("🚀 Your Infinity Metrics installation is ready!")
	fmt.Println("Thank you for choosing Infinity Metrics for your analytics needs.")

	os.Stdout.Sync() // Force flush to ensure output is captured
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

func runRestoreDB(inst *installer.Installer, logger *logging.Logger, startTime time.Time) {
	logger.Info("Starting database restore...")
	
	backupDir := inst.GetBackupDir()
	mainDBPath := inst.GetMainDBPath()
	
	// List available backups
	backups, err := inst.ListBackups()
	if err != nil {
		logger.Error("Failed to list backups: %v", err)
		os.Exit(1)
	}
	
	if len(backups) == 0 {
		logger.Error("No backups found in %s", backupDir)
		os.Exit(1)
	}
	
	// Let user select a backup
	selectedBackup, err := inst.PromptBackupSelection(backups)
	if err != nil {
		logger.Error("Backup selection failed: %v", err)
		os.Exit(1)
	}
	
	// Validate the selected backup
	if err := inst.ValidateBackup(selectedBackup); err != nil {
		logger.Error("Backup validation failed: %v", err)
		os.Exit(1)
	}
	
	// Confirmation prompt
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("⚠️  This will replace your current database with the selected backup.\n")
	fmt.Printf("   Current database: %s\n", mainDBPath)
	fmt.Printf("   Selected backup: %s\n", selectedBackup)
	fmt.Print("Are you sure you want to continue? (yes/no): ")
	
	confirmation, err := reader.ReadString('\n')
	if err != nil {
		logger.Error("Failed to read confirmation: %v", err)
		os.Exit(1)
	}
	
	confirmation = strings.TrimSpace(strings.ToLower(confirmation))
	if confirmation != "yes" && confirmation != "y" {
		logger.Info("Restore cancelled by user")
		os.Exit(0)
	}
	
	// Perform the restore
	err = inst.RestoreFromBackup(selectedBackup)
	if err != nil {
		logger.Error("Restore failed: %v", err)
		os.Exit(1)
	}
	
	elapsedTime := time.Since(startTime).Round(time.Second)
	logger.Success("Database restored successfully in %s", elapsedTime)
	logger.Info("Verify the installation by running: sudo docker ps | grep infinity-metrics")
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
	startTime := time.Now()
	adminMgr := admin.NewManager(logger)
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Enter admin email: ")
	emailInput, err := reader.ReadString('\n')
	if err != nil {
		logger.Error("Failed to read email: %v", err)
		return err
	}
	email := strings.TrimSpace(emailInput)
	if err := validation.ValidateEmail(email); err != nil {
		logger.Error("Invalid email: %v", err)
		return errors.WrapWithContext(err, "email validation failed")
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
		if err := validation.ValidatePassword(password); err != nil {
			fmt.Printf("Error: %v\n", err)
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

	elapsed := time.Since(startTime).Round(time.Second)
	logger.Success("Password changed in %s", elapsed)
	return nil
}

func printVersion() {
	fmt.Println(currentInstallerVersion)
}

func printUsage() {
	fmt.Println("Usage: infinity-metrics [command]")
	fmt.Println("\nCommands:")
	fmt.Println("  install                     Install Infinity Metrics")
	fmt.Println("  update                      Update an existing installation")
	fmt.Println("  reload                      Reload containers with latest .env config without backup")
	fmt.Println("  restore-db                  Interactively restore database from a backup")
	fmt.Println("  change-admin-password       Change the admin user password")
	fmt.Println("  version                     Show version information")
	fmt.Println("  help                        Show this help message")
}
