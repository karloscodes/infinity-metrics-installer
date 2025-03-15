package main

import (
	"flag"
	"os"
	"time"

	"infinity-metrics-installer/internal/logging"
	"infinity-metrics-installer/internal/updater"
)

func main() {
	startTime := time.Now()

	// Define command line flags
	logLevel := flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	verbose := flag.Bool("verbose", false, "Enable verbose output")
	noColor := flag.Bool("no-color", false, "Disable colored output")
	forceUpdate := flag.Bool("force", false, "Force update even if no changes detected")
	backupFlag := flag.Bool("backup", true, "Create database backup before updating")
	flag.Parse()

	// Initialize logger
	logger := logging.NewLogger(logging.Config{
		Level:    *logLevel,
		NoColor:  *noColor,
		Verbose:  *verbose,
		ShowTime: true,
	})

	logger.Info("Starting Infinity Metrics Updater")
	logger.Debug("Initializing update environment")

	// Create new updater instance with logger and options
	update := updater.NewUpdater(
		updater.WithLogger(logger),
		updater.WithForceUpdate(*forceUpdate),
		updater.WithBackup(*backupFlag),
	)

	// Run the update with progress reporting
	logger.Info("Beginning update process")
	if err := update.Run(); err != nil {
		logger.Error("Update failed: %s", err)
		os.Exit(1)
	}

	elapsedTime := time.Since(startTime).Round(time.Second)
	logger.Success("Update completed successfully in %s!", elapsedTime)
	logger.Info("Your Infinity Metrics installation is now up to date")
}
