package main

import (
	"flag"
	"fmt"
	"os"
	"time"

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
		runInstall(inst, logger, startTime)
	case "update":
		runUpdate(inst, logger, startTime)
	case "restore":
		runRestore(inst, logger, startTime)
	case "help":
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

	fmt.Println("Please provide the required configuration details:")
	config := config.NewConfig(logger)
	if err := config.CollectFromUser(); err != nil {
		logger.Error("Failed to collect configuration: %v", err)
		os.Exit(1)
	}
	logger.Success("Configuration collected from user")

	stop := logger.StartSpinner("Running installation...")
	err := inst.RunWithConfig(config)
	logger.StopSpinner(stop, err == nil, "Installation %s", Ternary(err == nil, "completed", "failed"))
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
	stop := logger.StartSpinner("Running update...")
	err := updater.Run(currentInstallerVersion)
	logger.StopSpinner(stop, err == nil, "Update %s", Ternary(err == nil, "completed", "failed"))
	if err != nil {
		logger.Error("Update failed: %v", err)
		os.Exit(1)
	}

	elapsedTime := time.Since(startTime).Round(time.Second)
	logger.Success("Update completed in %s", elapsedTime)
}

func runRestore(inst *installer.Installer, logger *logging.Logger, startTime time.Time) {
	fmt.Println("Starting Infinity Metrics Restore")

	stop := logger.StartSpinner("Running restore...")
	err := inst.Restore()
	logger.StopSpinner(stop, err == nil, "Restore %s", Ternary(err == nil, "completed", "failed"))
	if err != nil {
		logger.Error("Restore failed: %v", err)
		os.Exit(1)
	}

	elapsedTime := time.Since(startTime).Round(time.Second)
	logger.Success("Restore completed in %s", elapsedTime)
	data := inst.GetConfig().GetData()
	logger.InfoWithTime("Infinity Metrics restored, access at https://%s", data.Domain)
}

func printUsage(logger *logging.Logger) {
	fmt.Println("------------------------------------------------------------------")
	fmt.Printf("  Infinity Metrics Installer v%s - https://getinfinitymetrics.com\n", currentInstallerVersion)
	fmt.Println("------------------------------------------------------------------")
	fmt.Println("\nUsage:")
	fmt.Println("  infinity-metrics-installer [command] [flags]")
	fmt.Println("\nCommands:")
	fmt.Println("  install  Install Infinity Metrics")
	fmt.Println("  update   Update Infinity Metrics")
	fmt.Println("  restore  Restore Infinity Metrics")
	fmt.Println("  help     Show this help message")
	fmt.Println("\nFlags:")
	flag.PrintDefaults()
	fmt.Println("------------------------------------------------------------------")
}

func Ternary(condition bool, trueVal, falseVal string) string {
	if condition {
		return trueVal
	}
	return falseVal
}
