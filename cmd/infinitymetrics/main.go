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
	if len(os.Args) > 1 && os.Args[1] == "update" {
		logger.Info("Starting Infinity Metrics Update")
		logger.Debug("Initializing update environment")
		if err := inst.Update(currentInstallerVersion); err != nil {
			logger.Error("Update failed: %v", err)
			os.Exit(1)
		}
		elapsedTime := time.Since(startTime).Round(time.Second)
		logger.Success("Update completed successfully in %s!", elapsedTime)
	} else if len(os.Args) > 1 && os.Args[1] == "install" {
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
	} else {
		logger.Error("Please specify 'install' or 'update' as an argument")
		os.Exit(1)
	}
}
