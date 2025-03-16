package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"infinity-metrics-installer/internal/installer"
	"infinity-metrics-installer/internal/logging"
)

const currentInstallerVersion = "1.0.0" // Update per release

func main() {
	startTime := time.Now()

	// Define command line flags
	logLevel := flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	verbose := flag.Bool("verbose", false, "Enable verbose output")
	noColor := flag.Bool("no-color", false, "Disable colored output")
	flag.Parse()

	// Initialize logger
	logger := logging.NewLogger(logging.Config{
		Level:    *logLevel,
		NoColor:  *noColor,
		Verbose:  *verbose,
		ShowTime: true,
	})

	// Create installer instance
	inst := installer.NewInstaller(logger)

	// Command dispatch
	if len(os.Args) < 2 {
		printUsage(logger)
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "install":
		runInstall(inst, logger, startTime)
	case "update":
		runUpdate(inst, logger, startTime)
	case "restore":
		runRestore(inst, logger, startTime)
	default:
		logger.Error("Invalid command: %s", command)
		printUsage(logger)
		os.Exit(1)
	}
}

func runInstall(inst *installer.Installer, logger *logging.Logger, startTime time.Time) {
	logger.Info("Starting Infinity Metrics Installation")
	logger.Debug("Initializing installation environment")
	logger.Info("Beginning installation process")

	if err := inst.Run(); err != nil {
		logger.Error("Installation failed: %v", err)
		os.Exit(1)
	}

	elapsedTime := time.Since(startTime).Round(time.Second)
	logger.Success("Installation completed successfully in %s!", elapsedTime)
	logger.Info("Infinity Metrics is now deployed")
	data := inst.GetConfig().GetData()
	logger.Info("Access your dashboard at https://%s", data.Domain)
}

func runUpdate(inst *installer.Installer, logger *logging.Logger, startTime time.Time) {
	logger.Info("Starting Infinity Metrics Update")
	logger.Debug("Initializing update environment")

	if err := inst.Update(currentInstallerVersion); err != nil {
		logger.Error("Update failed: %v", err)
		os.Exit(1)
	}

	elapsedTime := time.Since(startTime).Round(time.Second)
	logger.Success("Update completed successfully in %s!", elapsedTime)
}

func runRestore(inst *installer.Installer, logger *logging.Logger, startTime time.Time) {
	logger.Info("Starting Infinity Metrics Restore")

	if err := inst.Restore(); err != nil {
		logger.Error("Restore failed: %v", err)
		os.Exit(1)
	}

	elapsedTime := time.Since(startTime).Round(time.Second)
	logger.Success("Restore completed successfully in %s!", elapsedTime)
	data := inst.GetConfig().GetData()
	logger.Info("Infinity Metrics restored, access at https://%s", data.Domain)
}

func printUsage(logger *logging.Logger) {
	fmt.Println("------------------------------------------------------------------")
	fmt.Println("  Infinity Metrics Installer - https://getinfinitymetrics.com")
	fmt.Println("------------------------------------------------------------------")
	fmt.Println("\nUsage:")
	fmt.Println("  infinity-metrics-installer [command] [flags]")
	fmt.Println("\nCommands:")
	fmt.Println("  install  Install Infinity Metrics")
	fmt.Println("  update   Update Infinity Metrics")
	fmt.Println("  restore  Restore Infinity Metrics")
	fmt.Println("\nFlags:")
	flag.PrintDefaults()
	fmt.Println("------------------------------------------------------------------")
	logger.Error("Please specify 'install', 'update', or 'restore' as an argument")
	fmt.Println("------------------------------------------------------------------")
}
