package main

import (
	"flag"
	"os"
	"time"

	"infinity-metrics-installer/internal/installer"
	"infinity-metrics-installer/internal/logging"
)

func main() {
	startTime := time.Now()

	// Define command line flags
	logLevel := flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	verbose := flag.Bool("verbose", false, "Enable verbose output")
	noColor := flag.Bool("no-color", false, "Disable colored output")
	configFile := flag.String("config", "", "Path to configuration file")
	flag.Parse()

	// Initialize logger
	logger := logging.NewLogger(logging.Config{
		Level:    *logLevel,
		NoColor:  *noColor,
		Verbose:  *verbose,
		ShowTime: true,
	})

	logger.Info("Starting Infinity Metrics Installer")
	logger.Debug("Initializing installation environment")

	// Create new installer instance with logger
	install := installer.NewInstaller(
		installer.WithLogger(logger),
		installer.WithConfigFile(*configFile),
	)

	// Run the installation with progress updates
	logger.Info("Beginning installation process")
	if err := install.Run(); err != nil {
		logger.Error("Installation failed: %s", err)
		os.Exit(1)
	}

	elapsedTime := time.Since(startTime).Round(time.Second)
	logger.Success("Installation completed successfully in %s!", elapsedTime)
	logger.Info("Infinity Metrics is now being deployed")
	logger.Info("You can access your dashboard at https://<your-domain>")
}
