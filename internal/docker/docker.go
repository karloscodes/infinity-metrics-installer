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

type Docker struct {
	logger *logging.Logger
}

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
	_, err = exec.Command("systemctl", "start", "docker").Run()
	if err != nil {
		return fmt.Errorf("start failed: %w", err)
	}
	_, err = exec.Command("systemctl", "enable", "docker").Run()
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
	caddyContent := d.generateCaddyfile(data, "infinity-app-1")
	if err := os.WriteFile(caddyFile, []byte(caddyContent), 0o644); err != nil {
		return fmt.Errorf("write Caddyfile: %w", err)
	}

	for _, image := range []string{data.AppImage, data.CaddyImage} {
		d.logger.Info("Pulling %s", image)
		for i := 0; i < 3; i++ {
			if _, err := d.runCommand("pull", image); err == nil {
				break
			} else if i == 2 {
				return fmt.Errorf("pull %s failed after retries: %w", image, err)
			}
			time.Sleep(5 * time.Second)
		}
	}

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
			"-e", "APP_NAME=infinity-app-1",
			"--memory=256m",
			"--restart", "unless-stopped",
			data.CaddyImage,
		)
		if err != nil {
			return fmt.Errorf("start caddy: %w", err)
		}
	}

	return d.deployApp(data, "infinity-app-1")
}

func (d *Docker) Update(conf *config.Config) error {
	data := conf.GetData()
	dataDir := data.InstallDir

	for _, image := range []string{data.AppImage, data.CaddyImage} {
		d.logger.Info("Pulling %s", image)
		for i := 0; i < 3; i++ {
			if _, err := d.runCommand("pull", image); err == nil {
				break
			} else if i == 2 {
				return fmt.Errorf("pull %s failed after retries: %w", image, err)
			}
			time.Sleep(5 * time.Second)
		}
	}

	if err := d.backupSQLite(dataDir, "infinity-app-1"); err != nil {
		d.logger.Warn("Backup failed, proceeding: %v", err)
	}

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

	caddyFile := dataDir + "/Caddyfile"
	caddyContent := d.generateCaddyfile(data, newName)
	if err := os.WriteFile(caddyFile, []byte(caddyContent), 0o644); err != nil {
		d.stopAndRemove(newName)
		return fmt.Errorf("write updated Caddyfile: %w", err)
	}
	if err := d.updateCaddyConfig("infinity-caddy", caddyContent); err != nil {
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
		"--memory=512m",
		"--restart", "unless-stopped",
		data.AppImage,
	)
	if err != nil {
		return fmt.Errorf("deploy %s: %w", err)
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
	if stat, err := os.Stat(backupFile); err != nil || stat.Size() == 0 {
		return fmt.Errorf("backup file invalid: %w", err)
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

func (d *Docker) generateCaddyfile(data config.ConfigData, appName string) string {
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
        hide /data /config
        header Cache-Control "public, max-age=31536000"
    }
    
    reverse_proxy %s:8080 {
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
        transport http {
            read_timeout 10s
            write_timeout 10s
            idle_timeout 60s
        }
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
}`, data.AdminEmail, data.Domain, data.Domain, data.Domain, data.AdminEmail, appName, data.Domain)
}
