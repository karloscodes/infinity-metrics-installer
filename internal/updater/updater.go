package updater

import (
	"fmt"
	"path/filepath"

	"infinity-metrics-installer/internal/config"
	"infinity-metrics-installer/internal/docker"
	"infinity-metrics-installer/internal/logging"
)

type Updater struct {
	logger *logging.Logger
	config *config.Config
	docker *docker.Docker
}

func NewUpdater(logger *logging.Logger) *Updater {
	return &Updater{
		logger: logger,
		config: config.NewConfig(logger),
		docker: docker.NewDocker(logger),
	}
}

func (i *Updater) Run() error {
	totalSteps := 3

	i.logger.Step(1, totalSteps, "Loading configuration")
	data := i.config.GetData()
	envFile := filepath.Join(data.InstallDir, ".env")
	if err := i.config.LoadFromFile(envFile); err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	i.logger.Step(2, totalSteps, "Checking for updates")
	if err := i.config.FetchFromServer("https://yourdomain.com/config.json"); err != nil {
		i.logger.Warn("Server config fetch failed, using local: %v", err)
	}

	i.logger.Step(3, totalSteps, "Applying updates")
	if err := i.docker.Update(i.config); err != nil {
		return fmt.Errorf("failed to update: %w", err)
	}

	if err := i.config.SaveToFile(envFile); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	i.logger.Success("Update completed successfully")
	return nil
}
