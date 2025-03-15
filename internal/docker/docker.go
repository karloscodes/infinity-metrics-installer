package docker

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"infinity-metrics-installer/internal/config"
	"infinity-metrics-installer/internal/logging"
)

// Docker manages Docker operations
type Docker struct {
	logger *logging.Logger
}

// NewDocker creates a Docker manager
func NewDocker(logger *logging.Logger) *Docker {
	return &Docker{logger: logger}
}

func (d *Docker) runCommand(args ...string) (string, error) {
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
	if _, err := d.runCommand("version"); err == nil {
		d.logger.Success("Docker is installed")
		return nil
	}

	d.logger.Info("Installing Docker...")
	_, err := exec.Command("bash", "-c", "curl -fsSL https://get.docker.com | sh").CombinedOutput()
	if err != nil {
		return fmt.Errorf("install failed: %w", err)
	}
	err = exec.Command("systemctl", "start", "docker").Run()
	if err != nil {
		return fmt.Errorf("start failed: %w", err)
	}
	err = exec.Command("systemctl", "enable", "docker").Run()
	if err != nil {
		return fmt.Errorf("enable failed: %w", err)
	}

	_, err = d.runCommand("version")
	if err != nil {
		return fmt.Errorf("verification failed: %w", err)
	}
	d.logger.Success("Docker installed")
	return nil
}

func (d *Docker) Deploy(conf *config.Config) error {
	data := conf.GetData()
	dataDir := data.InstallDir

	// Create directories
	for _, dir := range []string{
		dataDir + "/storage",
		dataDir + "/logs",
		dataDir + "/caddy",
		dataDir + "/caddy/config",
		dataDir + "/storage/backups",
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create dir %s: %w", dir, err)
		}
	}

	// Write initial Caddyfile
	caddyFile := dataDir + "/Caddyfile"
	if err := os.WriteFile(caddyFile, []byte("localhost:80 {\n  reverse_proxy infinity-app-1:8080\n}"), 0o644); err != nil {
		return fmt.Errorf("write Caddyfile: %w", err)
	}

	// Pull images
	d.logger.Info("Pulling %s", data.AppImage)
	if _, err := d.runCommand("pull", "always", data.AppImage); err != nil {
		return fmt.Errorf("pull app: %w", err)
	}
	d.logger.Info("Pulling %s", data.CaddyImage)
	if _, err := d.runCommand("pull", data.CaddyImage); err != nil {
		return fmt.Errorf("pull caddy: %w", err)
	}

	// Start Caddy
	caddyName := "infinity-caddy"
	if !d.isRunning(caddyName) {
		d.stopAndRemove(caddyName)
		_, err := d.runCommand("run", "-d",
			"--name", caddyName,
			"-p", "80:80", "-p", "443:443", "-p", "443:443/udp",
			"-v", caddyFile+":/etc/caddy/Caddyfile:ro",
			"-v", dataDir+"/caddy:/data",
			"-v", dataDir+"/caddy/config:/config",
			"-v", dataDir+"/logs:/data/logs",
			"-e", "DOMAIN="+data.Domain,
			"-e", "ADMIN_EMAIL="+data.AdminEmail,
			"--restart", "unless-stopped",
			data.CaddyImage,
		)
		if err != nil {
			return fmt.Errorf("start caddy: %w", err)
		}
	}

	// Start app
	return d.deployApp(data, "infinity-app-1")
}

func (d *Docker) Update(conf *config.Config) error {
	data := conf.GetData()
	dataDir := data.InstallDir

	// Pull new images
	d.logger.Info("Pulling %s", data.AppImage)
	if _, err := d.runCommand("pull", data.AppImage); err != nil {
		return fmt.Errorf("pull app: %w", err)
	}
	d.logger.Info("Pulling %s", data.CaddyImage)
	if _, err := d.runCommand("pull", data.CaddyImage); err != nil {
		return fmt.Errorf("pull caddy: %w", err)
	}

	// Backup SQLite
	if err := d.backupSQLite(dataDir, "infinity-app-1"); err != nil {
		d.logger.Warn("Backup failed, proceeding: %v", err)
	}

	// Rolling update
	currentName := "infinity-app-1"
	newName := "infinity-app-2"
	if d.isRunning(newName) {
		currentName, newName = newName, currentName
	}

	d.logger.Info("Starting %s", newName)
	if err := d.deployApp(data, newName); err != nil {
		d.stopAndRemove(newName)
		return fmt.Errorf("start new: %w", err)
	}

	// Health check
	for i := 0; i < 10; i++ {
		if _, err := d.runCommand("exec", newName, "curl", "-f", "http://localhost:8080/_health"); err == nil {
			break
		}
		time.Sleep(2 * time.Second)
		if i == 9 {
			d.logger.Error("Health check failed for %s", newName)
			d.stopAndRemove(newName)
			return fmt.Errorf("new container unhealthy")
		}
	}

	// Update Caddy
	caddyConfig := fmt.Sprintf("%s:80 {\n  reverse_proxy %s:8080\n}", data.Domain, newName)
	if err := d.updateCaddyConfig("infinity-caddy", caddyConfig); err != nil {
		d.stopAndRemove(newName)
		return fmt.Errorf("update caddy: %w", err)
	}

	d.stopAndRemove(currentName)
	d.runCommand("image", "prune", "-f")
	d.logger.Success("Update completed")
	return nil
}

func (d *Docker) deployApp(data config.ConfigData, name string) error {
	d.stopAndRemove(name)
	_, err := d.runCommand("run", "-d",
		"--name", name,
		"-v", data.InstallDir+"/storage:/app/storage",
		"-v", data.InstallDir+"/logs:/app/logs",
		"-e", "INFINITY_METRICS_LOG_LEVEL=info",
		"-e", "INFINITY_METRICS_APP_PORT=8080",
		"-e", "INFINITY_METRICS_LICENSE_KEY="+data.LicenseKey,
		"-e", "SERVER_INSTANCE_ID="+name,
		"--restart", "unless-stopped",
		data.AppImage,
	)
	if err != nil {
		return fmt.Errorf("deploy %s: %w", name, err)
	}
	return nil
}

func (d *Docker) backupSQLite(dataDir, appName string) error {
	timestamp := time.Now().Format("20060102150405")
	backupFile := fmt.Sprintf("%s/backup-%s.db", dataDir+"/storage/backups", timestamp)
	_, err := d.runCommand("exec", appName, "cp", "/app/storage/infinity-metrics-production.db", backupFile)
	if err != nil {
		return fmt.Errorf("backup sqlite: %w", err)
	}
	d.logger.Info("SQLite backed up to %s", backupFile)
	return nil
}

func (d *Docker) stopAndRemove(name string) {
	d.runCommand("stop", name)
	d.runCommand("rm", name)
}

func (d *Docker) updateCaddyConfig(caddyName, caddyConfig string) error {
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

func (d *Docker) isRunning(name string) bool {
	out, err := d.runCommand("ps", "-q", "-f", "name="+name)
	return err == nil && strings.TrimSpace(out) != ""
}
