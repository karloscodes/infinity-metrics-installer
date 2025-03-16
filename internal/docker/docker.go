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
	if _, err := d.RunCommand("version"); err == nil {
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

	_, err = d.RunCommand("version")
	if err != nil {
		return fmt.Errorf("verification failed: %w", err)
	}
	d.logger.Success("Docker installed")
	return nil
}

func (d *Docker) Deploy(conf *config.Config) error {
	data := conf.GetData()
	dataDir := data.InstallDir

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

	caddyFile := dataDir + "/Caddyfile"
	caddyContent := d.generateCaddyfile(data, "infinity-app-1", "")
	if err := os.WriteFile(caddyFile, []byte(caddyContent), 0o644); err != nil {
		return fmt.Errorf("write Caddyfile: %w", err)
	}

	for _, image := range []string{data.AppImage, data.CaddyImage} {
		d.logger.Info("Pulling %s", image)
		for i := 0; i < 3; i++ {
			if _, err := d.RunCommand("pull", image); err == nil {
				break
			} else if i == 2 {
				return fmt.Errorf("pull %s failed after retries: %w", image, err)
			}
			time.Sleep(5 * time.Second)
		}
	}

	caddyName := "infinity-caddy"
	if !d.IsRunning(caddyName) {
		d.StopAndRemove(caddyName)
		_, err := d.RunCommand("run", "-d",
			"--name", caddyName,
			"-p", "80:80", "-p", "443:443", "-p", "443:443/udp",
			"-v", caddyFile+":/etc/caddy/Caddyfile:ro",
			"-v", dataDir+"/caddy:/data",
			"-v", dataDir+"/caddy/config:/config",
			"-v", dataDir+"/logs:/data/logs",
			"-e", "DOMAIN="+data.Domain,
			"-e", "ADMIN_EMAIL="+data.AdminEmail,
			"-e", "APP_NAME=infinity-app-1",
			"--memory=256m",
			"--restart", "unless-stopped",
			data.CaddyImage,
		)
		if err != nil {
			return fmt.Errorf("start caddy: %w", err)
		}
	}

	return d.DeployApp(data, "infinity-app-1")
}

func (d *Docker) Update(conf *config.Config) error {
	data := conf.GetData()
	dataDir := data.InstallDir

	// Pull new images
	for _, image := range []string{data.AppImage, data.CaddyImage} {
		d.logger.Info("Pulling %s", image)
		for i := 0; i < 3; i++ {
			if _, err := d.RunCommand("pull", image); err == nil {
				break
			} else if i == 2 {
				return fmt.Errorf("pull %s failed after retries: %w", image, err)
			}
			time.Sleep(5 * time.Second)
		}
	}

	// Determine current and new containers
	currentName := "infinity-app-1"
	newName := "infinity-app-2"
	if d.IsRunning(newName) {
		currentName, newName = newName, currentName
	}

	// Backup SQLite database using database package
	mainDBPath := filepath.Join(dataDir, "storage", "infinity-metrics-production.db")
	backupDir := filepath.Join(dataDir, "storage", "backups")
	if _, err := d.db.BackupDatabase(mainDBPath, backupDir); err != nil {
		d.logger.Warn("Database backup failed: %v", err)
		d.logger.Warn("Proceeding with update without backup")
	}

	// Start new container with retries
	d.logger.Info("Starting %s", newName)
	for i := 0; i < 3; i++ {
		if err := d.DeployApp(data, newName); err == nil {
			break
		} else if i == 2 {
			d.StopAndRemove(newName)
			return fmt.Errorf("deploy %s failed after retries: %w", newName, err)
		}
		d.logger.Warn("Deploy %s failed, retrying (%d/3)", newName, i+1)
		d.StopAndRemove(newName)
		time.Sleep(2 * time.Second)
	}

	// Health check new container
	for i := 0; i < 5; i++ {
		if _, err := d.RunCommand("exec", newName, "curl", "-f", "http://localhost:8080/_health"); err == nil {
			break
		}
		time.Sleep(1 * time.Second)
		if i == 4 {
			d.logger.Error("Health check failed for %s", newName)
			d.StopAndRemove(newName)
			return fmt.Errorf("new container %s unhealthy", newName)
		}
	}

	// Stage 1: Update Caddyfile with both upstreams
	caddyFile := dataDir + "/Caddyfile"
	caddyContent := d.generateCaddyfile(data, currentName, newName)
	if err := os.WriteFile(caddyFile, []byte(caddyContent), 0o644); err != nil {
		d.StopAndRemove(newName)
		return fmt.Errorf("write transitional Caddyfile: %w", err)
	}
	if err := d.updateCaddyConfig("infinity-caddy", caddyContent); err != nil {
		d.StopAndRemove(newName)
		return fmt.Errorf("reload caddy with both upstreams: %w", err)
	}

	// Wait for traffic to stabilize
	time.Sleep(2 * time.Second)

	// Stage 2: Update Caddyfile to only new upstream
	caddyContent = d.generateCaddyfile(data, newName, "")
	if err := os.WriteFile(caddyFile, []byte(caddyContent), 0o644); err != nil {
		d.StopAndRemove(newName)
		return fmt.Errorf("write final Caddyfile: %w", err)
	}
	if err := d.updateCaddyConfig("infinity-caddy", caddyContent); err != nil {
		d.StopAndRemove(newName)
		return fmt.Errorf("reload caddy with new upstream: %w", err)
	}

	// Clean up
	d.StopAndRemove(currentName)
	d.RunCommand("image", "prune", "-f")
	d.logger.Success("Update completed with minimal downtime")
	return nil
}

func (d *Docker) DeployApp(data config.ConfigData, name string) error {
	d.StopAndRemove(name)
	_, err := d.RunCommand("run", "-d",
		"--name", name,
		"-v", data.InstallDir+"/storage:/app/storage",
		"-v", data.InstallDir+"/logs:/app/logs",
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

func (d *Docker) generateCaddyfile(data config.ConfigData, primaryApp, secondaryApp string) string {
	upstreams := primaryApp + ":8080"
	if secondaryApp != "" {
		upstreams += " " + secondaryApp + ":8080"
	}
	return fmt.Sprintf(`{
    admin off
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
    tls %s {
        protocols tls1.2 tls1.3
        curves x25519 secp256r1
        ciphers TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384 TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384
    }
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
}`, data.AdminEmail, data.Domain, data.Domain, data.Domain, data.AdminEmail, upstreams, data.Domain)
}
