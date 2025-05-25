#!/bin/bash

# Script to set up a Multipass VM for testing infinity-metrics-installer

# Configuration
VM_NAME="infinity-test-vm"
VM_MEMORY="2G"
VM_DISK="10G"
VM_CPUS="2"
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

# Check if the binary exists (but don't exit if it doesn't - it might be built later)
if [ ! -f "$INSTALLER_BINARY" ]; then
    echo -e "${RED}Warning: Installer binary not found at $INSTALLER_BINARY${NC}"
    echo "You may need to build the binary first with 'make build-linux'"
fi

# Check if VM already exists
if multipass info "$VM_NAME" &> /dev/null; then
    echo -e "${GREEN}VM '$VM_NAME' already exists${NC}"
    
    # Check if VM is running
    if multipass info "$VM_NAME" | grep -q "State:.*Running"; then
        echo -e "${GREEN}VM is already running${NC}"
    else
        echo "Starting VM..."
        multipass start "$VM_NAME" || {
            echo -e "${RED}Failed to start VM${NC}"
            exit 1
        }
        echo -e "${GREEN}VM started${NC}"
    fi
else
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
    
    # Update packages
    echo "Updating packages..."
    multipass exec "$VM_NAME" -- sudo apt-get update -q
    
    echo -e "${GREEN}VM setup complete${NC}"
fi

# Display VM information
echo -e "\n${GREEN}VM Information:${NC}"
multipass info "$VM_NAME"

# Display helpful commands
echo -e "\n${GREEN}Helpful commands:${NC}"
echo "  Connect to VM:    multipass shell $VM_NAME"
echo "  Delete VM:        multipass delete $VM_NAME --purge"
echo "  VM IP address:    multipass info $VM_NAME | grep IPv4"

echo -e "\n${GREEN}VM '$VM_NAME' is ready for testing${NC}"
