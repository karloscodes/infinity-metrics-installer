#!/bin/bash

set -e

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    echo "Please run this script as root (e.g., with sudo)"
    exit 1
fi

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
    x86_64)
        ARCH="amd64"
        ;;
    aarch64)
        ARCH="arm64"
        ;;
    *)
        echo "Unsupported architecture: $ARCH. Only amd64 and arm64 are supported."
        exit 1
        ;;
esac

# Define installation directory
INSTALL_DIR="/opt/infinity-metrics"
BINARY_URL="https://getinfinitymetrics.com/infinity-metrics-v1.0.0-$ARCH"  # Initial version

# Create installation directory
mkdir -p "$INSTALL_DIR"

# Download the binary
echo "Downloading Infinity Metrics for $ARCH..."
curl -sSL "$BINARY_URL" -o "$INSTALL_DIR/infinity-metrics"
chmod +x "$INSTALL_DIR/infinity-metrics"

# Run the install command
echo "Running installation..."
"$INSTALL_DIR/infinity-metrics" install

echo "Installation complete! You can now update with: sudo $INSTALL_DIR/infinity-metrics update"
