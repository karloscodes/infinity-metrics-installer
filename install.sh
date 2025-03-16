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

# GitHub repository info
GITHUB_REPO="karloscodes/infinity-metrics-installer"

# Fetch the latest release version
echo "Fetching latest release information..."
LATEST_VERSION=$(curl -s "https://api.github.com/repos/$GITHUB_REPO/releases/latest" | grep '"tag_name":' | sed -E 's/.*"v([^"]+)".*/\1/')

if [ -z "$LATEST_VERSION" ]; then
    echo "Could not determine latest version. Using default v1.0.0."
    LATEST_VERSION="1.0.0"
fi

echo "Latest version: $LATEST_VERSION"

# Construct the download URL for the binary
BINARY_URL="https://github.com/$GITHUB_REPO/releases/download/v$LATEST_VERSION/infinity-metrics-v$LATEST_VERSION-$ARCH"

# Create installation directory
mkdir -p "$INSTALL_DIR"

# Download the binary
echo "Downloading Infinity Metrics v$LATEST_VERSION for $ARCH..."
curl -sSL -o "$INSTALL_DIR/infinity-metrics" "$BINARY_URL"
chmod +x "$INSTALL_DIR/infinity-metrics"

# Run the install command
echo "Running installation..."
"$INSTALL_DIR/infinity-metrics" install
