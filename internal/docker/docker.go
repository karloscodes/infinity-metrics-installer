package docker

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	// Check if Docker is already installed
	if version, err := d.RunCommand("version"); err == nil {
		d.logger.Success("Docker is installed (version: %s)", strings.TrimSpace(strings.Split(version, "\n")[0]))
		return nil // Docker is already installed, no action needed
	}

	// Docker not found, proceed with installation
	d.logger.Info("Docker not found, installing...")
	output, err := exec.Command("bash", "-c", "curl -fsSL https://get.docker.com | sh").CombinedOutput()
	if err != nil {
		d.logger.Error("Docker installation failed: %s", string(output))
		return fmt.Errorf("install failed: %w", err)
	}
	d.logger.Success("Docker installed successfully")

	// Start and enable Docker service
	for _, cmd := range [][]string{
		{"systemctl", "start", "docker"},
		{"systemctl", "enable", "docker"},
	} {
		if err := exec.Command(cmd[0], cmd[1:]...).Run(); err != nil {
			return fmt.Errorf("%s failed: %w", cmd[1], err)
		}
	}

	// Verify installation
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

	// Check for active installation
	if d.IsRunning(CaddyName) && d.IsRunning(AppNamePrimary) {
		d.logger.Info("Active installation detected with running containers (%s, %s), skipping deployment", CaddyName, AppNamePrimary)
		return nil
	}

	// Create required directories
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

	// Ensure Docker network exists
	if _, err := d.RunCommand("network", "inspect", NetworkName); err != nil {
		d.logger.Info("Creating Docker network %s", NetworkName)
		if _, err := d.RunCommand("network", "create", NetworkName); err != nil {
			return fmt.Errorf("create network: %w", err)
		}
		d.logger.Success("Network created")
	}

	// Generate and write Caddyfile
	caddyFile := filepath.Join(dataDir, "Caddyfile")
	caddyContent := d.generateCaddyfile(data, AppNamePrimary, "")
	if err := os.WriteFile(caddyFile, []byte(caddyContent), 0o644); err != nil {
		return fmt.Errorf("write Caddyfile: %w", err)
	}

	// Pull Docker images
	for _, image := range []string{data.AppImage, data.CaddyImage} {
		d.logger.Info("Pulling %s...", image)
		for i := 0; i < MaxRetries; i++ {
			if _, err := d.RunCommand("pull", image); err == nil {
				d.logger.Success("%s pulled successfully", image)
				break
			} else if i == MaxRetries-1 {
				return fmt.Errorf("pull %s failed after %d retries: %w", image, MaxRetries, err)
			}
			d.logger.Warn("Pull %s failed, retrying (%d/%d)", image, i+1, MaxRetries)
			time.Sleep(time.Duration(i+1) * 2 * time.Second)
		}
	}

	// Deploy Caddy container if not already running
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

	return d.DeployApp(data, AppNamePrimary)
}

func (d *Docker) Update(conf *config.Config) error {
	data := conf.GetData()
	dataDir := data.InstallDir

	if _, err := d.RunCommand("network", "inspect", NetworkName); err != nil {
		d.logger.Info("Creating Docker network %s", NetworkName)
		if _, err := d.RunCommand("network", "create", NetworkName); err != nil {
			return fmt.Errorf("create network: %w", err)
		}
	}

	for _, image := range []string{data.AppImage, data.CaddyImage} {
		d.logger.Info("Pulling %s...", image)
		for i := 0; i < MaxRetries; i++ {
			if _, err := d.RunCommand("pull", image); err == nil {
				d.logger.Success("%s pulled successfully", image)
				break
			} else if i == MaxRetries-1 {
				return fmt.Errorf("pull %s failed after %d retries: %w", image, MaxRetries, err)
			}
			d.logger.Warn("Pull %s failed, retrying (%d/%d)", image, i+1, MaxRetries)
			time.Sleep(time.Duration(i+1) * 2 * time.Second)
		}
	}

	currentName := AppNamePrimary
	newName := AppNameSecondary
	if d.IsRunning(newName) {
		currentName, newName = newName, AppNamePrimary
	}

	mainDBPath := filepath.Join(dataDir, "storage", "infinity-metrics-production.db")
	backupDir := filepath.Join(dataDir, "storage", "backups")
	d.logger.Info("Backing up database...")
	if _, err := d.db.BackupDatabase(mainDBPath, backupDir); err != nil {
		d.logger.Warn("Proceeding without backup: %v", err)
	} else {
		d.logger.Success("Database backed up")
	}

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

	caddyFile := filepath.Join(dataDir, "Caddyfile")
	caddyContent := d.generateCaddyfile(data, currentName, newName)
	if err := os.WriteFile(caddyFile, []byte(caddyContent), 0o644); err != nil {
		d.StopAndRemove(newName)
		return fmt.Errorf("write transitional Caddyfile: %w", err)
	}

	d.logger.Info("Reloading Caddy with both upstreams...")

	if err := d.updateCaddyConfig(CaddyName, caddyContent); err != nil {
		d.StopAndRemove(newName)
		return fmt.Errorf("reload caddy with both upstreams: %w", err)
	}

	d.logger.Success("Caddy updated with both upstreams")

	time.Sleep(2 * time.Second)

	caddyContent = d.generateCaddyfile(data, newName, "")
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

	d.StopAndRemove(currentName)
	d.RunCommand("image", "prune", "-f")
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
		"-e", "INFINITY_METRICS_LOG_LEVEL=info",
		"-e", "INFINITY_METRICS_APP_PORT=8080",
		"-e", "INFINITY_METRICS_LICENSE_KEY="+data.LicenseKey,
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
	d.RunCommand("rm", name)
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

func (d *Docker) IsRunning(name string) bool {
	out, err := d.RunCommand("ps", "-q", "-f", "name="+name)
	return err == nil && strings.TrimSpace(out) != ""
}

func (d *Docker) ensureNetworkConnected(container, network string) error {
	// Inspect the network to see if the container is already connected
	output, err := d.RunCommand("network", "inspect", network, "--format", "{{range .Containers}}{{.Name}}{{end}}")
	if err != nil {
		return fmt.Errorf("failed to inspect network %s: %w", network, err)
	}

	// Check if the container is already connected to the network
	if strings.Contains(output, container) {
		d.logger.Info("Container %s is already connected to network %s", container, network)
		return nil
	}

	// Container is not connected, connect it to the network
	d.logger.Info("Connecting container %s to network %s...", container, network)
	_, err = d.RunCommand("network", "connect", network, container)
	if err != nil {
		return fmt.Errorf("failed to connect container %s to network %s: %w", container, network, err)
	}
	d.logger.Success("Container %s connected to network %s", container, network)

	return nil
}

func (d *Docker) generateCaddyfile(data config.ConfigData, primaryApp, secondaryApp string) string {
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

	return fmt.Sprintf(`{
    admin 127.0.0.1:2019
    email %s
    log {
        level INFO
        output file /data/logs/caddy.log {
            roll_size 50MiB
            roll_keep 5
            roll_keep_for 168h
        }
    }
    grace_period 30s
}

%s:80 {
    redir https://%s{uri} 301
}

%s:443 {
    tls %s
    encode zstd gzip
    
    file_server /assets/* {
        precompressed
    }
    
    reverse_proxy %s {
        health_uri /_health
        health_interval 10s
        health_timeout 5s
        health_status 200
        fail_duration 30s
        max_fails 2
        
        header_up X-Forwarded-Proto {scheme}
        header_up X-Forwarded-For {http.request.remote.host}
        header_up User-Agent {http.request.user_agent}
        header_up Referer {http.request.referer}
        header_up Accept-Language {http.request.header.Accept-Language}

        flush_interval -1
    }
    
    header {
        Strict-Transport-Security "max-age=31536000; includeSubDomains"
        X-Content-Type-Options "nosniff"
        X-Frame-Options "DENY"
        Content-Security-Policy "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'"
        Referrer-Policy "strict-origin-when-cross-origin"
        Permissions-Policy "microphone=(), camera=()"
        -Server
    }
    
    log {
        output file /data/logs/%s-access.log {
            roll_size 50MiB
            roll_keep 5
            roll_keep_for 168h
        }
        format json
    }
    
    handle_errors {
        @5xx expression {http.error.status_code} >= 500 && {http.error.status_code} <= 599
        respond @5xx "Service temporarily unavailable" 503
    }
}`, data.AdminEmail, data.Domain, data.Domain, data.Domain, tlsConfig, upstreams, data.Domain)
}
