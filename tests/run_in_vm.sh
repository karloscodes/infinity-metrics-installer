#!/bin/bash
set -e

# Infinity Metrics VM-based Integration Testing Script
# This script creates a VM, runs the installer, and verifies it works

# Default values
BINARY_PATH="./bin/infinity-metrics-installer"
UPDATER_PATH=""
VM_NAME="infinity-test-vm"
VM_MEMORY="2G"
VM_DISK="10G"
VM_CPUS="2"
KEEP_VM=false

# Parse command line arguments
while [[ $# -gt 0 ]]; do
  case $1 in
    --binary=*)
      BINARY_PATH="${1#*=}"
      shift
      ;;
    --updater=*)
      UPDATER_PATH="${1#*=}"
      shift
      ;;
    --vm-name=*)
      VM_NAME="${1#*=}"
      shift
      ;;
    --keep-vm)
      KEEP_VM=true
      shift
      ;;
    --memory=*)
      VM_MEMORY="${1#*=}"
      shift
      ;;
    --disk=*)
      VM_DISK="${1#*=}"
      shift
      ;;
    --cpus=*)
      VM_CPUS="${1#*=}"
      shift
      ;;
    *)
      echo "Unknown option: $1"
      exit 1
      ;;
  esac
done

# Check if binaries exist
if [ ! -f "$BINARY_PATH" ]; then
  echo "Error: Installer binary not found at $BINARY_PATH"
  exit 1
fi
if [ -z "$UPDATER_PATH" ] || [ ! -f "$UPDATER_PATH" ]; then
  echo "Error: Updater binary not specified or not found at $UPDATER_PATH"
  exit 1
fi

# Check binary type on host
echo "Checking local installer binary type:"
file "$BINARY_PATH"
echo "Checking local updater binary type:"
file "$UPDATER_PATH"

# Clean up function
cleanup() {
  if [ "$KEEP_VM" = false ]; then
    echo "Cleaning up the test VM..."
    multipass delete "$VM_NAME" --purge || true
  else
    echo "VM $VM_NAME kept for inspection."
    echo "To access: multipass shell $VM_NAME"
    echo "To delete: multipass delete $VM_NAME --purge"
  fi
}

# Set up trap to clean up on exit
trap cleanup EXIT

# Ensure old VM is deleted
echo "Ensuring no previous test VM exists..."
multipass delete "$VM_NAME" --purge 2>/dev/null || true

# Launch a new VM
echo "Launching test VM with Ubuntu 22.04..."
multipass launch 22.04 --name "$VM_NAME" --memory "$VM_MEMORY" --disk "$VM_DISK" --cpus "$VM_CPUS"

# Wait for VM to be ready
echo "Waiting for VM to be fully ready..."
sleep 30
VM_STATE=$(multipass info "$VM_NAME" | grep State | awk '{print $2}')
if [ "$VM_STATE" != "Running" ]; then
  echo "Error: VM is not in Running state: $VM_STATE"
  exit 1
fi

# Copy binaries to VM
echo "Copying installer binary to VM..."
multipass transfer "$BINARY_PATH" "$VM_NAME:/home/ubuntu/infinity-metrics-installer"
echo "Copying updater binary to VM..."
multipass transfer "$UPDATER_PATH" "$VM_NAME:/home/ubuntu/infinity-metrics-updater"

# Verify file transfers
echo "Verifying installer file transfer..."
ORIG_SIZE=$(stat -c %s "$BINARY_PATH" 2>/dev/null || stat -f %z "$BINARY_PATH")
VM_SIZE=$(multipass exec "$VM_NAME" -- stat -c %s /home/ubuntu/infinity-metrics-installer)
echo "Original installer size: $ORIG_SIZE bytes, VM file size: $VM_SIZE bytes"
if [ "$ORIG_SIZE" -ne "$VM_SIZE" ]; then
  echo "Error: Installer file size mismatch after transfer"
  exit 1
fi
echo "Verifying updater file transfer..."
ORIG_UPDATER_SIZE=$(stat -c %s "$UPDATER_PATH" 2>/dev/null || stat -f %z "$UPDATER_PATH")
VM_UPDATER_SIZE=$(multipass exec "$VM_NAME" -- stat -c %s /home/ubuntu/infinity-metrics-updater)
echo "Original updater size: $ORIG_UPDATER_SIZE bytes, VM file size: $VM_UPDATER_SIZE bytes"
if [ "$ORIG_UPDATER_SIZE" -ne "$VM_UPDATER_SIZE" ]; then
  echo "Error: Updater file size mismatch after transfer"
  exit 1
fi

# Make binaries executable
echo "Making installer executable..."
multipass exec "$VM_NAME" -- chmod +x /home/ubuntu/infinity-metrics-installer
echo "Making updater executable..."
multipass exec "$VM_NAME" -- chmod +x /home/ubuntu/infinity-metrics-updater

# Create and copy test config file
echo "Creating test configuration..."
cat > test_config.env << EOF
DOMAIN=test.infinitymetrics.local
ADMIN_EMAIL=admin@infinitymetrics.local
INFINITY_METRICS_LICENSE_KEY=TEST-LICENSE-KEY
ENABLE_BACKUPS=false
DOCKER_REGISTRY=localhost
TAG=latest
EOF
multipass transfer test_config.env "$VM_NAME:/home/ubuntu/test_config.env"
rm test_config.env

# Install binaries
echo "Copying installer to system location..."
multipass exec "$VM_NAME" -- sudo cp /home/ubuntu/infinity-metrics-installer /usr/local/bin/infinity-installer
multipass exec "$VM_NAME" -- sudo chmod +x /usr/local/bin/infinity-installer
echo "Copying updater to installation directory..."
multipass exec "$VM_NAME" -- sudo mkdir -p /opt/infinity-metrics
multipass exec "$VM_NAME" -- sudo cp /home/ubuntu/infinity-metrics-updater /opt/infinity-metrics/infinity-metrics-updater
multipass exec "$VM_NAME" -- sudo chmod +x /opt/infinity-metrics/infinity-metrics-updater

# Run installer with timeout and capture output
echo "Running installer with 120-second timeout..."
set +e # Temporarily disable set -e
INSTALLER_OUTPUT=$(multipass exec "$VM_NAME" -- timeout 120s sudo /usr/local/bin/infinity-installer --config /home/ubuntu/test_config.env 2>&1)
INSTALLER_EXIT_CODE=$?
set -e # Re-enable set -e
echo "$INSTALLER_OUTPUT" # Print installer output

if [ $INSTALLER_EXIT_CODE -ne 0 ]; then
  echo "Error: Installer failed with exit code $INSTALLER_EXIT_CODE"
  multipass exec "$VM_NAME" -- cat /var/log/syslog | tail -n 50
  exit 1
fi

# Verify installation with command outputs
echo "Checking Docker installation..."
multipass exec "$VM_NAME" -- docker --version
multipass exec "$VM_NAME" -- sudo systemctl status docker

echo "Checking Docker Swarm status..."
multipass exec "$VM_NAME" -- docker info
multipass exec "$VM_NAME" -- docker node ls

echo "Checking Infinity Metrics stack deployment..."
multipass exec "$VM_NAME" -- docker stack ls
multipass exec "$VM_NAME" -- docker stack ps infinity-metrics

# Check services with timeout
echo "Checking service status..."
TIMEOUT=300
START_TIME=$(date +%s)
while true; do
  CURRENT_TIME=$(date +%s)
  ELAPSED=$((CURRENT_TIME - START_TIME))
  echo "Elapsed time waiting for services: $ELAPSED seconds"
  
  if [ $ELAPSED -gt $TIMEOUT ]; then
    echo "Error: Timeout waiting for services to start after $TIMEOUT seconds"
    multipass exec "$VM_NAME" -- docker service ls
    multipass exec "$VM_NAME" -- docker ps -a
    exit 1
  fi
  
  SERVICE_STATUS=$(multipass exec "$VM_NAME" -- docker service ls)
  echo "Current service status:"
  echo "$SERVICE_STATUS"
  
  if echo "$SERVICE_STATUS" | grep -q "infinity-metrics" && ! echo "$SERVICE_STATUS" | grep -q "0/"; then
    echo "All services appear to be running!"
    break
  else
    echo "Some services are not fully started yet. Waiting..."
    sleep 5
  fi
done

# Print VM IP
if [ "$KEEP_VM" = true ]; then
  VM_IP=$(multipass info "$VM_NAME" | grep IPv4 | awk '{print $2}')
  echo "VM is running at IP: $VM_IP"
fi

echo "Integration test completed successfully!"
exit 0
