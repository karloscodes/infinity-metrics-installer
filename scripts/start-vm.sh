#!/bin/bash

# Script to set up a Multipass VM for testing infinity-metrics-installer

# Configuration
VM_NAME="infinity-test-vm"
VM_MEMORY="1G"
VM_DISK="5G"
VM_CPUS="1"
INSTALLER_BINARY="./bin/infinity-metrics"  # Adjust this path if your binary is elsewhere
UBUNTU_VERSION="22.04"                    # LTS version

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m' # No Color

# Check if Multipass is installed
if ! command -v multipass &> /dev/null; then
    echo -e "${RED}Error: Multipass is not installed.${NC}"
    echo "Please install Multipass first: https://multipass.run/install"
    echo "On macOS: brew install --cask multipass"
    echo "On Ubuntu: sudo snap install multipass"
    exit 1
fi

# Check if the binary exists
if [ ! -f "$INSTALLER_BINARY" ]; then
    echo -e "${RED}Error: Installer binary not found at $INSTALLER_BINARY${NC}"
    echo "Please build the binary first (e.g., 'go build -o $INSTALLER_BINARY') and adjust the INSTALLER_BINARY path in this script."
    exit 1
fi

# Launch the VM
echo "Launching Multipass VM '$VM_NAME'..."
multipass launch "$UBUNTU_VERSION" \
    --name "$VM_NAME" \
    --memory "$VM_MEMORY" \
    --disk "$VM_DISK" \
    --cpus "$VM_CPUS" || {
    echo -e "${RED}Failed to launch VM${NC}"
    exit 1
}

# Wait for VM to be ready
echo "Waiting for VM to start..."
until multipass info "$VM_NAME" | grep -q "State:.*Running"; do
    sleep 2
done
echo -e "${GREEN}VM '$VM_NAME' is running${NC}"
