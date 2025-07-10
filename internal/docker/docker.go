package docker

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"infinity-metrics-installer/internal/config"
	"infinity-metrics-installer/internal/database"
	"infinity-metrics-installer/internal/logging"
)

const (
	NetworkName      = "infinity-network"
	CaddyName        = "infinity-caddy"
	AppNamePrimary   = "infinity-app-1"
	AppNameSecondary = "infinity-app-2"
	MaxRetries       = 3
	HealthCheckTries = 5
)

//go:embed templates/Caddyfile.tmpl
var caddyfileTemplate string

type Docker struct {
	logger *logging.Logger
	db     *database.Database
}

func NewDocker(logger *logging.Logger, db *database.Database) *Docker {
	return &Docker{
		logger: logger,
		db:     db,
	}
}

func (d *Docker) RunCommand(args ...string) (string, error) {
	d.logger.Debug("Running docker %s", strings.Join(args, " "))
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("docker", args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("docker %s failed: %w - %s", args[0], err, stderr.String())
	}
	return stdout.String(), nil
}

func (d *Docker) EnsureInstalled() error {
	if version, err := d.RunCommand("version"); err == nil {
		d.logger.Success("Docker is installed (version: %s)", strings.TrimSpace(strings.Split(version, "\n")[0]))
		return nil
	}

	d.logger.Info("Docker not found, installing...")
	output, err := exec.Command("bash", "-c", "curl -fsSL https://get.docker.com | sh").CombinedOutput()
	if err != nil {
		d.logger.Error("Docker installation failed: %s", string(output))
		return fmt.Errorf("install failed: %w", err)
	}
	d.logger.Success("Docker installed successfully")

	for _, cmd := range [][]string{
		{"systemctl", "start", "docker"},
		{"systemctl", "enable", "docker"},
	} {
		if err := exec.Command(cmd[0], cmd[1:]...).Run(); err != nil {
			return fmt.Errorf("%s failed: %w", cmd[1], err)
		}
	}

	version, err := d.RunCommand("version")
	if err != nil {
		return fmt.Errorf("verification failed: %w", err)
	}
	d.logger.InfoWithTime("Docker version: %s", strings.TrimSpace(strings.Split(version, "\n")[0]))
	return nil
}

func (d *Docker) Deploy(conf *config.Config) error {
	data := conf.GetData()
	dataDir := data.InstallDir

	if d.IsRunning(CaddyName) && (d.IsRunning(AppNamePrimary) || d.IsRunning(AppNameSecondary)) {
		d.logger.Info("Active installation detected with running containers, skipping deployment")
		return nil
	}

	for _, dir := range []string{
		filepath.Join(dataDir, "storage"),
		filepath.Join(dataDir, "logs"),
		filepath.Join(dataDir, "caddy"),
		filepath.Join(dataDir, "caddy", "config"),
		filepath.Join(dataDir, "storage", "backups"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create dir %s: %w", dir, err)
		}
	}

	if _, err := d.RunCommand("network", "inspect", NetworkName); err != nil {
		d.logger.Info("Creating Docker network %s", NetworkName)
		if _, err := d.RunCommand("network", "create", NetworkName); err != nil {
			return fmt.Errorf("create network: %w", err)
		}
		d.logger.Success("Network created")
	}

	caddyFile := filepath.Join(dataDir, "Caddyfile")
	caddyContent, err := d.generateCaddyfile(data)
	if err != nil {
		return fmt.Errorf("generate Caddyfile: %w", err)
	}
	if err := os.WriteFile(caddyFile, []byte(caddyContent), 0o644); err != nil {
		return fmt.Errorf("write Caddyfile: %w", err)
	}

	for _, image := range []string{data.AppImage, data.CaddyImage} {
		d.logger.Info("Pulling %s...", image)
		for i := 0; i < MaxRetries; i++ {
			if _, err := d.RunCommand("pull", image); err == nil {
				d.logger.Success("%s pulled successfully", image)
				d.logImageDigest(image)
				break
			} else if i == MaxRetries-1 {
				return fmt.Errorf("pull %s failed after %d retries: %w", image, MaxRetries, err)
			}
			d.logger.Warn("Pull %s failed, retrying (%d/%d)", image, i+1, MaxRetries)
			time.Sleep(time.Duration(i+1) * 2 * time.Second)
		}
	}

	// Deploy app first
	if err := d.DeployApp(data, AppNamePrimary); err != nil {
		return fmt.Errorf("initial app deploy failed: %w", err)
	}

	if err := d.waitForAppHealth(AppNamePrimary); err != nil {
		d.StopAndRemove(AppNamePrimary)
		return fmt.Errorf("app %s not healthy: %w", AppNamePrimary, err)
	}

	if !d.IsRunning(CaddyName) {
		if err := d.deployCaddy(data, caddyFile); err != nil {
			return fmt.Errorf("deploy caddy: %w", err)
		}
	} else {
		if err := d.ensureNetworkConnected(CaddyName, NetworkName); err != nil {
			return fmt.Errorf("failed to ensure network for %s: %w", CaddyName, err)
		}
	}

	d.logCaddyVersion()
	return nil
}

func (d *Docker) Update(conf *config.Config) error {
	data := conf.GetData()
	dataDir := data.InstallDir

	if _, err := d.RunCommand("network", "inspect", NetworkName); err != nil {
		d.logger.Info("Creating Docker network %s", NetworkName)
		if _, err := d.RunCommand("network", "create", NetworkName); err != nil {
			return fmt.Errorf("create network: %w", err)
		}
		d.logger.Success("Network created")
	}

	// Pull new images using the unified DockerImages struct
	dockerImages := conf.GetDockerImages()
	for _, image := range []string{dockerImages.AppImage, dockerImages.CaddyImage} {
		// Check if we need to pull the image
		shouldPull, err := d.ShouldPullImage(image)
		if err != nil {
			d.logger.Warn("Error checking image status for %s: %v, will attempt to pull", image, err)
			shouldPull = true
		}

		if shouldPull {
			d.logger.Info("Pulling %s...", image)
			for i := 0; i < MaxRetries; i++ {
				if _, err := d.RunCommand("pull", image); err == nil {
					d.logger.Success("%s pulled successfully", image)
					d.logImageDigest(image)
					break
				} else if i == MaxRetries-1 {
					return fmt.Errorf("pull %s failed after %d retries: %w", image, MaxRetries, err)
				}
				d.logger.Warn("Pull %s failed, retrying (%d/%d)", image, i+1, MaxRetries)
				time.Sleep(time.Duration(i+1) * 2 * time.Second)
			}
		} else {
			d.logger.Success("Image %s is already up to date, skipping pull", image)
			// Still log the digest for consistency in logs
			d.logImageDigest(image)
		}
	}

	// Determine current and new app instances
	currentName := AppNamePrimary
	newName := AppNameSecondary
	if d.IsRunning(AppNameSecondary) && !d.IsRunning(AppNamePrimary) {
		currentName, newName = AppNameSecondary, AppNamePrimary
	}

	// Deploy the new app instance
	for i := 0; i < MaxRetries; i++ {
		if err := d.DeployApp(data, newName); err == nil {
			d.logger.Success("%s deployed", newName)
			break
		} else if i == MaxRetries-1 {
			d.logger.Error("Failed to deploy %s after %d retries", newName, MaxRetries)
			// If the container was created but failed to start properly, try to get logs
			if d.containerExists(newName) {
				d.logContainerLogs(newName)
			}
			d.StopAndRemove(newName)
			return fmt.Errorf("deploy %s failed after %d retries: %w", newName, MaxRetries, err)
		}
		d.logger.Warn("Deploy %s failed, retrying (%d/%d)", newName, i+1, MaxRetries)
		d.StopAndRemove(newName)
		time.Sleep(time.Duration(i+1) * time.Second)
	}

	if err := d.ensureNetworkConnected(newName, NetworkName); err != nil {
		d.StopAndRemove(newName)
		return fmt.Errorf("failed to ensure network for %s: %w", newName, err)
	}

	if err := d.waitForAppHealth(newName); err != nil {
		d.StopAndRemove(newName)
		return fmt.Errorf("new app %s not healthy: %w", newName, err)
	}

	// Redeploy Caddy to ensure it uses the new image
	d.logger.Info("Redeploying Caddy with new image...")
	caddyFile := filepath.Join(dataDir, "Caddyfile")
	caddyContent, err := d.generateCaddyfile(data)
	if err != nil {
		return fmt.Errorf("generate Caddyfile: %w", err)
	}
	if err := os.WriteFile(caddyFile, []byte(caddyContent), 0o644); err != nil {
		return fmt.Errorf("write Caddyfile: %w", err)
	}
	d.logger.Info("Reloading Caddy configuration to point to %s...", newName)
	if _, err := d.RunCommand("exec", CaddyName, "caddy", "reload", "--config", "/etc/caddy/Caddyfile"); err != nil {
		d.logger.Warn("Caddy reload failed: %v. Attempting full Caddy redeploy as a fallback.", err)
		// Fallback to stop and redeploy if reload fails
		d.StopAndRemove(CaddyName)
		if errRedeploy := d.deployCaddy(data, caddyFile); errRedeploy != nil {
			return fmt.Errorf("caddy reload failed and subsequent redeploy also failed: %w (reload error: %v)", errRedeploy, err)
		}
		d.logger.Info("Caddy successfully redeployed as a fallback.")
	} else {
		d.logger.Success("Caddy configuration reloaded successfully")
	}

	d.logCaddyVersion()
	d.logContainerImage(newName)

	// Clean up old app instance
	d.StopAndRemove(currentName)
	d.RunCommand("image", "prune", "-f")

	return nil
}

func (d *Docker) Reload(conf *config.Config) error {
	data := conf.GetData()
	dataDir := data.InstallDir

	d.logger.Info("Starting container reload with latest environment variables")

	// Ensure network exists
	if _, err := d.RunCommand("network", "inspect", NetworkName); err != nil {
		d.logger.Info("Creating Docker network %s", NetworkName)
		if _, err := d.RunCommand("network", "create", NetworkName); err != nil {
			return fmt.Errorf("create network: %w", err)
		}
		d.logger.Success("Network created")
	}

	// Find which app container is running
	currentName := ""
	if d.IsRunning(AppNamePrimary) {
		currentName = AppNamePrimary
	} else if d.IsRunning(AppNameSecondary) {
		currentName = AppNameSecondary
	} else {
		d.logger.Warn("No app container running, will deploy primary")
		currentName = AppNamePrimary
	}

	d.logger.Info("Restarting app container: %s", currentName)
	d.StopAndRemove(currentName)

	// Deploy the app container
	if err := d.DeployApp(data, currentName); err != nil {
		return fmt.Errorf("failed to redeploy app container %s: %w", currentName, err)
	}

	if err := d.waitForAppHealth(currentName); err != nil {
		d.StopAndRemove(currentName)
		return fmt.Errorf("app %s not healthy after restart: %w", currentName, err)
	}

	// Restart Caddy container
	if d.IsRunning(CaddyName) {
		d.logger.Info("Restarting Caddy container")

		caddyFile := filepath.Join(dataDir, "Caddyfile")
		caddyContent, err := d.generateCaddyfile(data)
		if err != nil {
			return fmt.Errorf("generate Caddyfile: %w", err)
		}

		// Write the Caddyfile
		if err := os.WriteFile(caddyFile, []byte(caddyContent), 0o644); err != nil {
			return fmt.Errorf("write Caddyfile: %w", err)
		}

		// Reload Caddy
		d.logger.Info("Reloading Caddy configuration with new environment variables...")
		if _, err := d.RunCommand("exec", CaddyName, "caddy", "reload", "--config", "/etc/caddy/Caddyfile"); err != nil {
			d.logger.Warn("Caddy reload failed: %v. Attempting full Caddy redeploy as a fallback.", err)
			// Fallback to stop and redeploy if reload fails
			d.StopAndRemove(CaddyName)
			if errRedeploy := d.deployCaddy(data, caddyFile); errRedeploy != nil {
				return fmt.Errorf("caddy reload failed and subsequent redeploy also failed: %w (reload error: %v)", errRedeploy, err)
			}
			d.logger.Info("Caddy successfully redeployed as a fallback.")
		} else {
			d.logger.Success("Caddy configuration reloaded successfully")
		}
	}

	d.logger.Success("Containers reloaded successfully with new environment variables")
	return nil
}

func (d *Docker) deployCaddy(data config.ConfigData, caddyFile string) error {
	d.StopAndRemove(CaddyName)
	d.logger.Info("Starting Caddy container...")
	_, err := d.RunCommand("run", "-d",
		"--name", CaddyName,
		"--network", NetworkName,
		"--pull", "always",
		"-p", "80:80", "-p", "443:443", "-p", "443:443/udp",
		"-v", caddyFile+":/etc/caddy/Caddyfile:ro",
		"-v", filepath.Join(data.InstallDir, "caddy")+":/data",
		"-v", filepath.Join(data.InstallDir, "caddy", "config")+":/config",
		"-v", filepath.Join(data.InstallDir, "logs")+":/data/logs",
		"-e", "DOMAIN="+data.Domain,
		"-e", "ADMIN_EMAIL="+data.AdminEmail,
		"--memory=256m",
		"--restart", "unless-stopped",
		data.CaddyImage,
	)
	if err != nil {
		return fmt.Errorf("start caddy: %w", err)
	}
	d.logger.Success("Caddy deployed")

	d.logger.Info("Ensuring /data directory is writable in %s container...", CaddyName)
	_, err = d.RunCommand("exec", CaddyName, "chmod", "-R", "755", "/data")
	if err != nil {
		return fmt.Errorf("failed to set permissions on /data directory in %s container: %w", CaddyName, err)
	}
	d.logger.Success("/data directory permissions ensured")
	return nil
}

func (d *Docker) DeployApp(data config.ConfigData, name string) error {
	d.StopAndRemove(name)
	d.logger.Info("Deploying %s...", name)
	_, err := d.RunCommand("run", "-d",
		"--name", name,
		"--network", NetworkName,
		"--pull", "always",
		"-v", filepath.Join(data.InstallDir, "storage")+":/app/storage",
		"-v", filepath.Join(data.InstallDir, "logs")+":/app/logs",
		"-e", "INFINITY_METRICS_LOG_LEVEL=debug",
		"-e", "INFINITY_METRICS_APP_PORT=8080",
		"-e", "INFINITY_METRICS_LICENSE_KEY="+data.LicenseKey,
		"-e", "INFINITY_METRICS_DOMAIN="+data.Domain,
		"-e", "INFINITY_METRICS_PRIVATE_KEY="+data.PrivateKey,
		"-e", "SERVER_INSTANCE_ID="+name,
		"--memory=512m",
		"--restart", "unless-stopped",
		data.AppImage,
	)
	if err != nil {
		return fmt.Errorf("deploy %s: %w", name, err)
	}
	d.logger.Success("%s deployed", name)
	return nil
}

func (d *Docker) StopAndRemove(name string) {
	d.RunCommand("stop", name)
	d.RunCommand("rm", "-f", name)
}

func (d *Docker) IsRunning(name string) bool {
	out, err := d.RunCommand("ps", "-q", "-f", "name="+name)
	return err == nil && strings.TrimSpace(out) != ""
}

func (d *Docker) ExecuteCommand(command ...string) error {
	containerName := AppNamePrimary
	if !d.IsRunning(containerName) {
		containerName = AppNameSecondary
		if !d.IsRunning(containerName) {
			return fmt.Errorf("no running app container found")
		}
	}

	args := []string{"exec", containerName}
	args = append(args, command...)

	d.logger.Debug("Executing in app container %s: %s", containerName, strings.Join(command, " "))

	var stdout, stderr bytes.Buffer
	cmd := exec.Command("docker", args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to execute in container %s: %w - %s", containerName, err, stderr.String())
	}

	if stdout.Len() > 0 {
		d.logger.Debug("Command output: %s", stdout.String())
	}

	return nil
}

func (d *Docker) ensureNetworkConnected(container, network string) error {
	output, err := d.RunCommand("network", "inspect", network, "--format", "{{range .Containers}}{{.Name}}{{end}}")
	if err != nil {
		d.logger.Error("Failed to inspect network %s: %v", network, err)
		return fmt.Errorf("failed to inspect network %s: %w", network, err)
	}

	if strings.Contains(output, container) {
		d.logger.Info("Container %s is already connected to network %s", container, network)
		return nil
	}

	d.logger.Info("Connecting container %s to network %s...", container, network)
	_, err = d.RunCommand("network", "connect", network, container)
	if err != nil {
		d.logger.Error("Failed to connect container %s to network %s. Error: %v", container, network, err)

		// Check container status to provide more context
		if d.containerExists(container) {
			status, statusErr := d.RunCommand("inspect", "--format", "{{.State.Status}}", container)
			if statusErr == nil {
				d.logger.Info("Container %s current status: %s", container, strings.TrimSpace(status))
			}
		} else {
			d.logger.Error("Container %s no longer exists, which may be why network connection failed", container)
		}

		return fmt.Errorf("failed to connect container %s to network %s: %w", container, network, err)
	}
	d.logger.Success("Container %s connected to network %s", container, network)
	return nil
}

func (d *Docker) generateCaddyfile(data config.ConfigData) (string, error) {
	env := os.Getenv("ENV")
	var tlsConfig string
	if env == "test" {
		d.logger.Info("Using self-signed certificate for test environment")
		tlsConfig = "internal"
	} else {
		d.logger.Info("Using Let's Encrypt for production environment")
		tlsConfig = data.AdminEmail
	}

	tplData := struct {
		AdminEmail string
		Domain     string
		TLSConfig  string
	}{
		AdminEmail: data.AdminEmail,
		Domain:     data.Domain,
		TLSConfig:  tlsConfig,
	}

	tmpl, err := template.New("caddyfile").Parse(caddyfileTemplate)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, tplData); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}

	d.logger.Debug("Generated Caddyfile: %s", buf.String())
	return buf.String(), nil
}

func (d *Docker) waitForAppHealth(name string) error {
	d.logger.Info("Waiting for %s to become healthy...", name)
	for i := 0; i < HealthCheckTries; i++ {
		if _, err := d.RunCommand("exec", name, "curl", "-f", "http://localhost:8080/_health"); err == nil {
			d.logger.Success("%s is healthy", name)
			return nil
		}
		time.Sleep(2 * time.Second)
		if i == HealthCheckTries-1 {
			d.logger.Error("Container %s failed to become healthy after %d attempts", name, HealthCheckTries)
			d.logContainerLogs(name)
			return fmt.Errorf("app %s not healthy after %d attempts", name, HealthCheckTries)
		}
	}
	return nil
}

func (d *Docker) logContainerLogs(containerName string) {
	d.logger.Warn("Fetching logs from unhealthy container %s to diagnose issue:", containerName)

	logs, err := d.RunCommand("logs", "--tail", "50", containerName)
	if err != nil {
		d.logger.Error("Failed to fetch logs for container %s: %v", containerName, err)
		return
	}

	if logs == "" {
		d.logger.Warn("No logs available for container %s", containerName)
	} else {
		for _, line := range strings.Split(logs, "\n") {
			if line != "" {
				d.logger.Debug("[Container %s] %s", containerName, line)
			}
		}
	}

	status, err := d.RunCommand("inspect", "--format", "{{.State.Status}}", containerName)
	if err == nil {
		d.logger.Info("Container %s current status: %s", containerName, strings.TrimSpace(status))
	}

	errMsg, err := d.RunCommand("inspect", "--format", "{{.State.Error}}", containerName)
	if err == nil && strings.TrimSpace(errMsg) != "" && strings.TrimSpace(errMsg) != "<no value>" {
		d.logger.Error("Container %s error message: %s", containerName, strings.TrimSpace(errMsg))
	}
}

func (d *Docker) logCaddyVersion() {
	output, err := d.RunCommand("exec", CaddyName, "caddy", "version")
	if err == nil {
		d.logger.Info("Caddy version: %s", strings.TrimSpace(output))
	} else {
		d.logger.Warn("Failed to get Caddy version: %v", err)
	}
}

func (d *Docker) logContainerImage(containerName string) {
	output, err := d.RunCommand("inspect", containerName, "--format", "{{.Config.Image}}")
	if err == nil {
		d.logger.Info("%s is running image: %s", containerName, strings.TrimSpace(output))
	} else {
		d.logger.Warn("Failed to inspect %s image: %v", containerName, err)
	}
}

func (d *Docker) logImageDigest(image string) {
	output, err := d.RunCommand("inspect", image, "--format", "{{.Id}}")
	if err == nil {
		d.logger.Info("Image %s digest: %s", image, strings.TrimSpace(output))
	} else {
		d.logger.Warn("Failed to get digest for %s: %v", image, err)
	}
}

func (d *Docker) containerExists(name string) bool {
	// Check if the container exists, even if it's not running
	out, err := d.RunCommand("ps", "-a", "-q", "-f", "name="+name)
	return err == nil && strings.TrimSpace(out) != ""
}
