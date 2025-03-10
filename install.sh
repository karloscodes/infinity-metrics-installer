#!/bin/bash
set -e

# Fusion Analytics Installer
# This script will install and configure Fusion Analytics on your server using Docker Swarm

# Define colors for console output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Installation directory
INSTALL_DIR="/opt/fusion-analytics"

echo -e "${BLUE}=====================================${NC}"
echo -e "${BLUE}   Fusion Analytics Installation     ${NC}"
echo -e "${BLUE}=====================================${NC}"
echo ""

# Function to check if command exists
command_exists() {
  command -v "$1" >/dev/null 2>&1
}

# Check and install dependencies
echo -e "${BLUE}Checking dependencies...${NC}"

# Check Docker
if ! command_exists docker; then
  echo -e "${BLUE}Installing Docker...${NC}"
  curl -fsSL https://get.docker.com -o get-docker.sh
  sh get-docker.sh
  rm get-docker.sh
  echo -e "${GREEN}Docker installed successfully${NC}"
else
  echo -e "${GREEN}Docker is already installed${NC}"
fi

# Check Docker Compose
if ! command_exists docker-compose; then
  echo -e "${BLUE}Installing Docker Compose...${NC}"
  mkdir -p /usr/local/lib/docker/cli-plugins
  curl -SL "https://github.com/docker/compose/releases/latest/download/docker-compose-$(uname -s)-$(uname -m)" -o /usr/local/lib/docker/cli-plugins/docker-compose
  chmod +x /usr/local/lib/docker/cli-plugins/docker-compose
  ln -sf /usr/local/lib/docker/cli-plugins/docker-compose /usr/local/bin/docker-compose
  echo -e "${GREEN}Docker Compose installed successfully${NC}"
else
  echo -e "${GREEN}Docker Compose is already installed${NC}"
fi

# Initialize Docker Swarm if not already in Swarm mode
if ! docker info | grep -q "Swarm: active"; then
  echo -e "${BLUE}Initializing Docker Swarm...${NC}"
  # Get the primary IP address of the server
  SERVER_IP=$(hostname -I | awk '{print $1}')
  docker swarm init --advertise-addr "$SERVER_IP"
  echo -e "${GREEN}Docker Swarm initialized${NC}"
else
  echo -e "${GREEN}Docker Swarm is already active${NC}"
fi

# Create installation directory
echo -e "${BLUE}Creating installation directory...${NC}"
mkdir -p "$INSTALL_DIR"
cd "$INSTALL_DIR"

# Function to validate domain
validate_domain() {
  local domain=$1
  if [[ ! "$domain" =~ ^[a-zA-Z0-9][a-zA-Z0-9\.-]*\.[a-zA-Z]{2,}$ ]]; then
    return 1
  fi
  return 0
}

# Function to validate email
validate_email() {
  local email=$1
  if [[ ! "$email" =~ ^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$ ]]; then
    return 1
  fi
  return 0
}

# Collect user inputs
echo -e "${BLUE}Please provide the following information:${NC}"

# License key validation
while true; do
  read -p "License key: " LICENSE_KEY
  if [ -z "$LICENSE_KEY" ]; then
    echo -e "${RED}License key cannot be empty. Please try again.${NC}"
    continue
  fi
done

# Domain input with validation
while true; do
  read -p "Domain name (e.g., analytics.example.com): " DOMAIN
  if validate_domain "$DOMAIN"; then
    break
  else
    echo -e "${RED}Invalid domain format. Please try again.${NC}"
  fi
done

# Email input with validation
while true; do
  read -p "Email address (for SSL certificates): " EMAIL
  if validate_email "$EMAIL"; then
    break
  else
    echo -e "${RED}Invalid email format. Please try again.${NC}"
  fi
done

# Optional S3 backup configuration
echo ""
read -p "Do you want to configure S3 backups? (y/n): " CONFIGURE_S3
if [[ "$CONFIGURE_S3" =~ ^[Yy]$ ]]; then
  read -p "S3 bucket name: " S3_BUCKET
  read -p "AWS region (e.g., us-east-1): " AWS_REGION
  read -p "AWS access key ID: " AWS_ACCESS_KEY
  read -p "AWS secret access key: " AWS_SECRET_KEY
  USING_S3=true
else
  USING_S3=false
fi

# Docker image location
DOCKER_IMAGE="fusionanalytics/analytics:latest"

# Create stack.yml for Docker Swarm
echo -e "${BLUE}Creating Docker Swarm stack configuration...${NC}"
cat > stack.yml << EOL
version: '3.8'

services:
  caddy:
    image: caddy:2.7-alpine
    deploy:
      mode: replicated
      replicas: 1
      update_config:
        order: start-first
        failure_action: rollback
        delay: 10s
      rollback_config:
        parallelism: 0
        order: stop-first
      restart_policy:
        condition: on-failure
        delay: 5s
        max_attempts: 3
        window: 120s
    ports:
      - "80:80"
      - "443:443"
      - "443:443/udp" # For HTTP/3
    volumes:
      - caddy_data:/data
      - caddy_config:/config
      - ./Caddyfile:/etc/caddy/Caddyfile:ro
    environment:
      - DOMAIN=\${DOMAIN}
      - ADMIN_EMAIL=\${ADMIN_EMAIL}
    networks:
      - fusion-network

  analytics:
    image: ${DOCKER_IMAGE}
    deploy:
      mode: replicated
      replicas: 1
      update_config:
        order: start-first
        failure_action: rollback
        delay: 10s
      rollback_config:
        parallelism: 0
        order: stop-first
      restart_policy:
        condition: on-failure
        delay: 5s
        max_attempts: 3
        window: 120s
    volumes:
      - analytics_storage:/app/storage
      - analytics_logs:/app/logs
      - ./litestream.yml:/app/litestream.yml:ro
    environment:
      - INFINITY_METRICS_ENV=production
      - INFINITY_METRICS_STORAGE_PATH=/app/storage
      - INFINITY_METRICS_GEO_DB_PATH=/app/storage/GeoLite2-City.mmdb
      - INFINITY_METRICS_PUBLIC_DIR=/app/web/dist
      - INFINITY_METRICS_LOGS_DIR=/app/logs
      - INFINITY_METRICS_LOG_LEVEL=info
      - INFINITY_METRICS_APP_PORT=8080
      - SERVER_INSTANCE_ID={{.Node.ID}}-{{.Service.ID}}
      - LICENSE_KEY=\${LICENSE_KEY}
      - TZ=UTC
      - ENABLE_BACKUPS=\${ENABLE_BACKUPS:-false}
      - AWS_ACCESS_KEY_ID=\${AWS_ACCESS_KEY_ID:-}
      - AWS_SECRET_ACCESS_KEY=\${AWS_SECRET_ACCESS_KEY:-}
      - S3_BUCKET=\${S3_BUCKET:-}
      - S3_REGION=\${S3_REGION:-}
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
      interval: 10s
      timeout: 5s
      retries: 3
      start_period: 5s
    networks:
      - fusion-network

volumes:
  caddy_data:
  caddy_config:
  analytics_storage:
    driver: local
  analytics_logs:
    driver: local

networks:
  fusion-network:
    driver: overlay
EOL

# Create Caddyfile
echo -e "${BLUE}Creating Caddy configuration...${NC}"
cat > Caddyfile << EOL
{
    # Global options
    admin off
    email {$ADMIN_EMAIL}
    log {
        level INFO
    }
}

# Main site configuration
{$DOMAIN} {
    # Enable compression
    encode gzip zstd

    # Forward requests to the analytics service
    reverse_proxy analytics:8080 {
        # Health checks
        health_uri /health
        health_interval 10s
        health_timeout 5s
        health_status 200
        
        # Load balancing if you decide to scale
        lb_policy round_robin
        
        # Handle failures gracefully
        fail_duration 10s
        max_fails 3
        unhealthy_status 502 503 504
    }

    # Security headers
    header {
        # Enable HTTP Strict Transport Security (HSTS)
        Strict-Transport-Security "max-age=31536000; includeSubDomains; preload"
        # Prevent MIME type sniffing
        X-Content-Type-Options "nosniff"
        # Clickjacking protection
        X-Frame-Options "DENY"
        # XSS Protection
        X-XSS-Protection "1; mode=block"
        # Referrer policy
        Referrer-Policy "strict-origin-when-cross-origin"
        # Remove Server header
        -Server
    }

    # Log requests
    log {
        output file /var/log/caddy/{$DOMAIN}-access.log {
            roll_size 10MB
            roll_keep 5
            roll_keep_for 168h
        }
    }
}
EOL

# Create Litestream configuration if using S3
if [ "$USING_S3" = true ]; then
  echo -e "${BLUE}Creating Litestream configuration...${NC}"
  cat > litestream.yml << EOL
access-key-id: \${AWS_ACCESS_KEY_ID}
secret-access-key: \${AWS_SECRET_ACCESS_KEY}

dbs:
  - path: /app/storage/infinity-metrics-production.db
    replicas:
      - url: s3://\${S3_BUCKET}/\${DOMAIN}/db
        region: \${S3_REGION}
        sync-interval: 1m
    snapshots:
      - url: s3://\${S3_BUCKET}/\${DOMAIN}/snapshots
        region: \${S3_REGION}
        retention: 336h    # Keep snapshots for 14 days
        interval: 24h      # Create daily snapshots

exec: 
  - name: infinity-metrics
    args: ["/app/infinity-metrics"]
    restart-on-exit: true
    restart-on-error: true
EOL
fi

# Create .env file for environment variables
echo -e "${BLUE}Creating environment configuration...${NC}"
cat > .env << EOL
# Fusion Analytics Configuration
DOMAIN=$DOMAIN
ADMIN_EMAIL=$EMAIL
LICENSE_KEY=$LICENSE_KEY
ENABLE_BACKUPS=$USING_S3
EOL

# Add S3 configuration if enabled
if [ "$USING_S3" = true ]; then
  cat >> .env << EOL
# S3 Backup Configuration
AWS_ACCESS_KEY_ID=$AWS_ACCESS_KEY
AWS_SECRET_ACCESS_KEY=$AWS_SECRET_KEY
S3_BUCKET=$S3_BUCKET
S3_REGION=$AWS_REGION
EOL
fi

# Create update script for Swarm deployment
echo -e "${BLUE}Creating update script...${NC}"
cat > update.sh << EOL
#!/bin/bash
set -e

# Fusion Analytics Update Script
echo "Updating Fusion Analytics..."

# Pull the latest Docker images
docker pull ${DOCKER_IMAGE}

# Deploy the updated stack with zero downtime
docker stack deploy -c stack.yml fusion-analytics

echo "Update completed successfully!"
EOL
chmod +x update.sh

# Create backup script
echo -e "${BLUE}Creating backup script...${NC}"
cat > backup.sh << EOL
#!/bin/bash
set -e

# Fusion Analytics Backup Script
TIMESTAMP=\$(date +%Y%m%d_%H%M%S)
BACKUP_DIR="/opt/fusion-analytics/backups"

echo "Creating database backup..."
mkdir -p "\$BACKUP_DIR"

# Find the container running the analytics service
CONTAINER_ID=\$(docker ps --filter name=fusion-analytics_analytics --format "{{.ID}}")

if [ -z "\$CONTAINER_ID" ]; then
  echo "Error: Analytics container not found!"
  exit 1
fi

# Create a backup using SQLite
docker exec \$CONTAINER_ID sqlite3 /app/storage/infinity-metrics-production.db ".backup '/app/storage/backup_\$TIMESTAMP.db'"
docker cp \$CONTAINER_ID:/app/storage/backup_\$TIMESTAMP.db "\$BACKUP_DIR/backup_\$TIMESTAMP.db"
docker exec \$CONTAINER_ID rm /app/storage/backup_\$TIMESTAMP.db

# Keep only the 10 most recent backups
find "\$BACKUP_DIR" -name "backup_*.db" -type f -printf '%T@ %p\n' | sort -n | head -n -10 | cut -d' ' -f2- | xargs -r rm

echo "Backup completed successfully: \$BACKUP_DIR/backup_\$TIMESTAMP.db"
EOL
chmod +x backup.sh

# Deploy the stack to Docker Swarm
echo -e "${BLUE}Deploying Fusion Analytics to Docker Swarm...${NC}"
export $(grep -v '^#' .env | xargs)
docker stack deploy -c stack.yml fusion-analytics

# Wait for services to start
echo -e "${BLUE}Waiting for services to start...${NC}"
sleep 10

# Check if services are running
if docker stack services fusion-analytics | grep -q "1/1"; then
  echo -e "${GREEN}=====================================${NC}"
  echo -e "${GREEN}   Fusion Analytics is now running!   ${NC}"
  echo -e "${GREEN}=====================================${NC}"
  echo ""
  echo -e "Access your analytics dashboard at: ${BLUE}https://$DOMAIN${NC}"
  echo ""
  echo -e "For documentation and support, visit: ${BLUE}https://docs.fusionanalytics.com${NC}"
  
  # Add cron job for auto-updates if not exists
  if ! crontab -l 2>/dev/null | grep -q "fusion-analytics/update.sh"; then
    echo -e "${BLUE}Setting up automatic updates...${NC}"
    (crontab -l 2>/dev/null; echo "0 3 * * * /opt/fusion-analytics/update.sh >> /opt/fusion-analytics/logs/updates.log 2>&1") | crontab -
    echo -e "${GREEN}Automatic updates configured${NC}"
  fi
  
  # Add backup cron job if using S3
  if [ "$USING_S3" = true ] && ! crontab -l 2>/dev/null | grep -q "fusion-analytics/backup.sh"; then
    echo -e "${BLUE}Setting up automatic backups...${NC}"
    (crontab -l 2>/dev/null; echo "0 2 * * * /opt/fusion-analytics/backup.sh >> /opt/fusion-analytics/logs/backups.log 2>&1") | crontab -
    echo -e "${GREEN}Automatic backups configured${NC}"
  fi
else
  echo -e "${RED}Error: Fusion Analytics deployment failed.${NC}"
  echo "Please check the logs with 'docker service logs fusion-analytics_analytics' for more information."
  exit 1
fi
