package main

import (
	"flag"
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

	// Check command argument
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "install":
			logger.Info("Starting Infinity Metrics Installer")
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

		case "update":
			logger.Info("Starting Infinity Metrics Update")
			logger.Debug("Initializing update environment")
			if err := inst.Update(currentInstallerVersion); err != nil {
				logger.Error("Update failed: %v", err)
				os.Exit(1)
			}
			elapsedTime := time.Since(startTime).Round(time.Second)
			logger.Success("Update completed successfully in %s!", elapsedTime)

		case "restore":
			logger.Info("Starting Infinity Metrics Restore")
			if err := inst.Restore(); err != nil {
				logger.Error("Restore failed: %v", err)
				os.Exit(1)
			}
			elapsedTime := time.Since(startTime).Round(time.Second)
			logger.Success("Restore completed successfully in %s!", elapsedTime)
			data := inst.GetConfig().GetData()
			logger.Info("Infinity Metrics restored, access at https://%s", data.Domain)

		default:
			logger.Error("Please specify 'install', 'update', or 'restore' as an argument")
			os.Exit(1)
		}
	} else {
		logger.Error("Please specify 'install', 'update', or 'restore' as an argument")
		os.Exit(1)
	}
}
