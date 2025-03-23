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

	if d.IsRunning(CaddyName) && d.IsRunning(AppNamePrimary) {
		d.logger.Info("Active installation detected with running containers (%s, %s), skipping deployment", CaddyName, AppNamePrimary)
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
	caddyContent, err := d.generateCaddyfile(data, AppNamePrimary, "")
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
				d.logImageDigest(image) // Log image digest to confirm
				break
			} else if i == MaxRetries-1 {
				return fmt.Errorf("pull %s failed after %d retries: %w", image, MaxRetries, err)
			}
			d.logger.Warn("Pull %s failed, retrying (%d/%d)", image, i+1, MaxRetries)
			time.Sleep(time.Duration(i+1) * 2 * time.Second)
		}
	}

	if !d.IsRunning(CaddyName) {
		d.StopAndRemove(CaddyName)
		d.logger.Info("Starting Caddy container...")
		_, err := d.RunCommand("run", "-d",
			"--name", CaddyName,
			"--network", NetworkName,
			"-p", "80:80", "-p", "443:443", "-p", "443:443/udp",
			"-v", caddyFile+":/etc/caddy/Caddyfile:ro",
			"-v", filepath.Join(dataDir, "caddy")+":/data",
			"-v", filepath.Join(dataDir, "caddy", "config")+":/config",
			"-v", filepath.Join(dataDir, "logs")+":/data/logs",
			"-e", "DOMAIN="+data.Domain,
			"-e", "ADMIN_EMAIL="+data.AdminEmail,
			"-e", "APP_NAME="+AppNamePrimary,
			"--memory=256m",
			"--restart", "unless-stopped",
			data.CaddyImage,
		)
		if err != nil {
			return fmt.Errorf("start caddy: %w", err)
		}
		d.logger.Success("Caddy deployed")
	} else {
		if err := d.ensureNetworkConnected(CaddyName, NetworkName); err != nil {
			return fmt.Errorf("failed to ensure network for %s: %w", CaddyName, err)
		}
	}

	d.logger.Info("Ensuring /data directory is writable in %s container...", CaddyName)
	_, err = d.RunCommand("exec", CaddyName, "chmod", "-R", "755", "/data")
	if err != nil {
		return fmt.Errorf("failed to set permissions on /data directory in %s container: %w", CaddyName, err)
	}
	d.logger.Success("/data directory permissions ensured")
	d.logCaddyVersion() // Log Caddy version after deployment

	return d.DeployApp(data, AppNamePrimary)
}

func (d *Docker) Update(conf *config.Config) error {
	data := conf.GetData()
	dataDir := data.InstallDir

	// Ensure network exists
	if _, err := d.RunCommand("network", "inspect", NetworkName); err != nil {
		d.logger.Info("Creating Docker network %s", NetworkName)
		if _, err := d.RunCommand("network", "create", NetworkName); err != nil {
			return fmt.Errorf("create network: %w", err)
		}
		d.logger.Success("Network created")
	}

	// Pull new images and ensure no caching
	for _, image := range []string{data.AppImage, data.CaddyImage} {
		d.logger.Info("Removing old image %s to avoid caching...", image)
		d.RunCommand("rmi", "-f", image) // Force remove old image
		d.logger.Info("Pulling %s...", image)
		for i := 0; i < MaxRetries; i++ {
			if _, err := d.RunCommand("pull", image); err == nil {
				d.logger.Success("%s pulled successfully", image)
				d.logImageDigest(image) // Log digest to confirm the exact image
				break
			} else if i == MaxRetries-1 {
				return fmt.Errorf("pull %s failed after %d retries: %w", image, MaxRetries, err)
			}
			d.logger.Warn("Pull %s failed, retrying (%d/%d)", image, i+1, MaxRetries)
			time.Sleep(time.Duration(i+1) * 2 * time.Second)
		}
	}

	// Stop and remove all containers to ensure a clean slate
	d.logger.Info("Stopping and removing all containers for update...")
	d.StopAndRemove(CaddyName)
	d.StopAndRemove(AppNamePrimary)
	d.StopAndRemove(AppNameSecondary)

	// Redeploy Caddy with the new image
	d.logger.Info("Redeploying Caddy container with new image...")
	caddyFile := filepath.Join(dataDir, "Caddyfile")
	caddyContent, err := d.generateCaddyfile(data, AppNamePrimary, "")
	if err != nil {
		return fmt.Errorf("generate Caddyfile: %w", err)
	}
	if err := os.WriteFile(caddyFile, []byte(caddyContent), 0o644); err != nil {
		return fmt.Errorf("write Caddyfile: %w", err)
	}
	if err := d.deployCaddy(data, caddyFile); err != nil {
		return fmt.Errorf("redeploy Caddy: %w", err)
	}
	d.logCaddyVersion() // Confirm Caddy version

	// Update app container with zero-downtime approach
	currentName := AppNamePrimary
	newName := AppNameSecondary

	for i := 0; i < MaxRetries; i++ {
		if err := d.DeployApp(data, newName); err == nil {
			d.logger.Success("%s deployed", newName)
			break
		} else if i == MaxRetries-1 {
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

	d.logger.Info("Checking %s health...", newName)
	for i := 0; i < HealthCheckTries; i++ {
		if _, err := d.RunCommand("exec", newName, "curl", "-f", "http://localhost:8080/_health"); err == nil {
			d.logger.Success("%s is healthy", newName)
			break
		}
		time.Sleep(1 * time.Second)
		if i == HealthCheckTries-1 {
			d.StopAndRemove(newName)
			return fmt.Errorf("new container %s unhealthy after %d attempts", newName, HealthCheckTries)
		}
	}

	// Update Caddy to point to the new app container
	caddyContent, err = d.generateCaddyfile(data, newName, "")
	if err != nil {
		d.StopAndRemove(newName)
		return fmt.Errorf("generate final Caddyfile: %w", err)
	}
	if err := os.WriteFile(caddyFile, []byte(caddyContent), 0o644); err != nil {
		d.StopAndRemove(newName)
		return fmt.Errorf("write final Caddyfile: %w", err)
	}

	d.logger.Info("Reloading Caddy with new upstream...")
	if err := d.updateCaddyConfig(CaddyName, caddyContent); err != nil {
		d.StopAndRemove(newName)
		return fmt.Errorf("reload caddy with new upstream: %w", err)
	}
	d.logger.Success("Caddy updated to new upstream")

	// Clean up old app container and prune images
	d.StopAndRemove(currentName)
	d.RunCommand("image", "prune", "-f")
	d.logContainerImage(newName) // Confirm app container image

	return nil
}

func (d *Docker) deployCaddy(data config.ConfigData, caddyFile string) error {
	d.StopAndRemove(CaddyName)
	d.logger.Info("Starting Caddy container...")
	_, err := d.RunCommand("run", "-d",
		"--name", CaddyName,
		"--network", NetworkName,
		"-p", "80:80", "-p", "443:443", "-p", "443:443/udp",
		"-v", caddyFile+":/etc/caddy/Caddyfile:ro",
		"-v", filepath.Join(data.InstallDir, "caddy")+":/data",
		"-v", filepath.Join(data.InstallDir, "caddy", "config")+":/config",
		"-v", filepath.Join(data.InstallDir, "logs")+":/data/logs",
		"-e", "DOMAIN="+data.Domain,
		"-e", "ADMIN_EMAIL="+data.AdminEmail,
		"-e", "APP_NAME="+AppNamePrimary,
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
		"-v", filepath.Join(data.InstallDir, "storage")+":/app/storage",
		"-v", filepath.Join(data.InstallDir, "logs")+":/app/logs",
		"-e", "INFINITY_METRICS_LOG_LEVEL=debug",
		"-e", "INFINITY_METRICS_APP_PORT=8080",
		"-e", "INFINITY_METRICS_GEO_DB_PATH=/app/storage/GeoLite2-City.mmdb",
		"-e", "INFINITY_METRICS_LICENSE_KEY="+data.LicenseKey,
		"-e", "INFINITY_METRICS_DOMAIN="+data.Domain,
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
	d.RunCommand("rm", "-f", name) // Force remove to ensure cleanup
}

func (d *Docker) updateCaddyConfig(caddyName, caddyConfig string) error {
	d.logger.Info("Ensuring /data directory is writable in %s container...", caddyName)
	_, err := d.RunCommand("exec", caddyName, "chmod", "-R", "755", "/data")
	if err != nil {
		return fmt.Errorf("failed to set permissions on /data directory in %s container: %w", caddyName, err)
	}
	d.logger.Success("/data directory permissions ensured")

	var buf bytes.Buffer
	buf.WriteString(caddyConfig)
	cmd := exec.Command("docker", "exec", caddyName, "caddy", "reload", "--config", "/dev/stdin")
	cmd.Stdin = &buf
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("reload caddy: %w - %s", err, stderr.String())
	}
	return nil
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
		return fmt.Errorf("failed to inspect network %s: %w", network, err)
	}

	if strings.Contains(output, container) {
		d.logger.Info("Container %s is already connected to network %s", container, network)
		return nil
	}

	d.logger.Info("Connecting container %s to network %s...", container, network)
	_, err = d.RunCommand("network", "connect", network, container)
	if err != nil {
		return fmt.Errorf("failed to connect container %s to network %s: %w", container, network, err)
	}
	d.logger.Success("Container %s connected to network %s", container, network)
	return nil
}

func (d *Docker) generateCaddyfile(data config.ConfigData, primaryApp, secondaryApp string) (string, error) {
	upstreams := primaryApp + ":8080"
	if secondaryApp != "" {
		upstreams += " " + secondaryApp + ":8080"
	}

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
		Upstreams  string
	}{
		AdminEmail: data.AdminEmail,
		Domain:     data.Domain,
		TLSConfig:  tlsConfig,
		Upstreams:  upstreams,
	}

	tmpl, err := template.New("caddyfile").Parse(caddyfileTemplate)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, tplData); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}

	return buf.String(), nil
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
