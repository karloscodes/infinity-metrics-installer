#!/bin/bash
set -e

# Infinity Metrics VM-based Integration Testing Script
# This script creates a VM, runs the infinity-metrics binary with a specified command, and verifies it works

# Default values
BINARY_PATH="./bin/infinity-metrics"
COMMAND="install"
VM_NAME="infinity-test-vm"
VM_MEMORY="2G"
VM_DISK="10G"
VM_CPUS="2"
KEEP_VM=false
DEBUG=${DEBUG:-0}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
  case $1 in
    --binary=*)
      BINARY_PATH="${1#*=}"
      shift
      ;;
    --args=*)
      COMMAND="${1#*=}"
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

# Check if binary exists
if [ ! -f "$BINARY_PATH" ]; then
  echo "Error: Binary not found at $BINARY_PATH"
  exit 1
fi

# Check binary type on host
echo "Checking local binary type:"
file "$BINARY_PATH"


# Ensure old VM is deleted
echo "Ensuring no previous test VM exists..."
multipass delete "$VM_NAME" --purge 2>/dev/null || true

# Launch a new VM
echo "Launching test VM with Ubuntu 22.04..."
multipass launch 22.04 --name "$VM_NAME" --memory "$VM_MEMORY" --disk "$VM_DISK" --cpus "$VM_CPUS"

# Wait for VM to be ready
echo "Waiting for VM to be fully ready..."
for i in {1..30}; do
  VM_STATE=$(multipass info "$VM_NAME" | grep State | awk '{print $2}' || echo "Unknown")
  if [ "$VM_STATE" = "Running" ]; then
    echo "VM is running after $i seconds"
    break
  fi
  if [ $i -eq 30 ]; then
    echo "Error: VM did not reach Running state after 30 seconds: $VM_STATE"
    exit 1
  fi
  sleep 1
done

# Copy binary to VM
echo "Copying binary to VM..."
multipass transfer "$BINARY_PATH" "$VM_NAME:/home/ubuntu/infinity-metrics"

# Verify file transfer
echo "Verifying file transfer..."
ORIG_SIZE=$(stat -c %s "$BINARY_PATH" 2>/dev/null || stat -f %z "$BINARY_PATH")
VM_SIZE=$(multipass exec "$VM_NAME" -- stat -c %s /home/ubuntu/infinity-metrics || echo "0")
echo "Original size: $ORIG_SIZE bytes, VM file size: $VM_SIZE bytes"
if [ "$ORIG_SIZE" -ne "$VM_SIZE" ]; then
  echo "Error: File size mismatch after transfer"
  exit 1
fi

# Make binary executable and move to system location
echo "Making binary executable and installing..."
multipass exec "$VM_NAME" -- chmod +x /home/ubuntu/infinity-metrics
multipass exec "$VM_NAME" -- sudo mv /home/ubuntu/infinity-metrics /usr/local/bin/infinity-metrics

# Run the binary with the specified command and timeout, providing input for install
echo "Running infinity-metrics $COMMAND with 300-second timeout..."
set +e
if [ "$COMMAND" = "install" ]; then
  # Pipe input for CollectFromUser: Domain, AdminEmail, LicenseKey
  COMMAND_OUTPUT=$(echo -e "localhost\nadmin@localhost\n${LICENSE_KEY}" | multipass exec "$VM_NAME" -- timeout 300s sudo /usr/local/bin/infinity-metrics "$COMMAND" 2>&1)
else
  COMMAND_OUTPUT=$(multipass exec "$VM_NAME" -- timeout 60s sudo /usr/local/bin/infinity-metrics "$COMMAND" 2>&1)
fi
COMMAND_EXIT_CODE=$?
set -e
echo "$COMMAND_OUTPUT"

if [ $COMMAND_EXIT_CODE -ne 0 ]; then
  echo "Error: infinity-metrics $COMMAND failed with exit code $COMMAND_EXIT_CODE"
  multipass exec "$VM_NAME" -- cat /opt/infinity-metrics/logs/infinity-metrics.log | tail -n 50
  exit 1
fi

echo "Integration test for $COMMAND completed successfully!"
exit 0
