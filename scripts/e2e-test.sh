#!/bin/bash
set -e

# End-to-end test script using VM providers
# Supports Multipass (all platforms) and UTM (Apple Silicon)

echo "🚀 Infinity Metrics E2E Tests"
echo "============================"

# Configuration
VM_NAME="infinity-test-vm-$(date +%s)"
DEBUG=${DEBUG:-0}
KEEP_VM=${KEEP_VM:-0}
SKIP_DNS_CHECKS=${SKIP_DNS_VALIDATION:-1}  # Default to skipping DNS

# Detect platform and choose VM provider
USE_UTM=0

# Check if VM_PROVIDER is set from the environment
if [[ -n "$VM_PROVIDER" ]]; then
  if [[ "$VM_PROVIDER" == "utm" ]]; then
    if command -v utm &>/dev/null; then
      USE_UTM=1
      echo "🍎 Using UTM as VM provider"
    else
      echo "⚠️  UTM requested but not found"
      echo "   Please install UTM from https://mac.getutm.app"
      exit 1
    fi
  else
    echo "Using Multipass as VM provider"
  fi
else
  # Auto-detect if not specified
  if [[ "$(uname -s)" == "Darwin" && "$(uname -m)" == "arm64" ]]; then
    if command -v utm &>/dev/null; then
      USE_UTM=1
      echo "🍎 Detected Apple Silicon Mac with UTM"
    else
      echo "⚠️  Running on Apple Silicon Mac without UTM"
      echo "   For best x86_64 VM performance, consider installing UTM from https://mac.getutm.app"
    fi
  fi
fi

# Parse command line arguments
while [[ $# -gt 0 ]]; do
  case $1 in
    --vm-name)
      VM_NAME="$2"
      shift 2
      ;;
    --debug)
      DEBUG=1
      shift
      ;;
    --keep-vm)
      KEEP_VM=1
      shift
      ;;
    --check-dns)
      SKIP_DNS_CHECKS=0
      shift
      ;;
    *)
      echo "Unknown option: $1"
      echo "Usage: $0 [--vm-name NAME] [--debug] [--keep-vm] [--check-dns]"
      exit 1
      ;;
  esac
done

# Helper function for debugging
log() {
  if [[ $DEBUG -eq 1 ]]; then
    echo "[DEBUG] $1"
  fi
}

# Build test binary
echo "🔧 Building test binary..."
mkdir -p bin
log "Building infinity-metrics binary..."
go build -o bin/infinity-metrics cmd/infinitymetrics/main.go || { 
  echo "❌ Build failed!"
  exit 1
}



# Check for required VM provider
if [[ $USE_UTM -eq 1 ]]; then
  if ! utm --version &>/dev/null; then
    echo "⚠️  UTM appears to be unresponsive"
    echo "   Please ensure UTM is installed and working properly"
    echo ""
    echo "Cannot continue without a working UTM installation"
    exit 1
  fi
else
  if ! multipass version &>/dev/null; then
    echo "⚠️  Multipass appears to be unresponsive"
    echo "   Try restarting the multipass daemon with:"
    echo "   sudo launchctl unload /Library/LaunchDaemons/com.canonical.multipassd.plist"
    echo "   sudo launchctl load /Library/LaunchDaemons/com.canonical.multipassd.plist"
    echo ""
    echo "Cannot continue without a working Multipass installation"
    exit 1
  fi
fi

# Clean up existing VM if needed
echo "🧹 Cleaning up any existing VM with the same name..."
if [[ $USE_UTM -eq 1 ]]; then
  utm stop $VM_NAME 2>/dev/null || true
  utm delete $VM_NAME 2>/dev/null || true
else
  multipass delete $VM_NAME --purge 2>/dev/null || true
fi

# Create VM
echo "🚀 Creating VM: $VM_NAME"
LAUNCH_ARGS=("--memory" "2G" "--disk" "10G" "--cpus" "2")



# Launch the VM
if [[ $USE_UTM -eq 1 ]]; then
  log "Launching VM with UTM: $VM_NAME"
  
  # Check if VM already exists
  if utm list | grep -q "$VM_NAME"; then
    log "VM already exists, starting it"
    utm start "$VM_NAME" || {
      echo "❌ Failed to start existing VM!"
      echo "Cannot continue without a working VM"
      exit 1
    }
  else
    # Download Ubuntu image if needed
    IMAGE_FILE="/tmp/ubuntu-22.04-server-amd64.iso"
    if [ ! -f "$IMAGE_FILE" ]; then
      echo "Downloading Ubuntu 22.04 server image..."
      curl -L -o "$IMAGE_FILE" "https://releases.ubuntu.com/22.04/ubuntu-22.04.3-live-server-amd64.iso" || {
        echo "❌ Failed to download Ubuntu image!"
        echo "Cannot continue without the Ubuntu image"
        exit 1
      }
    fi
    
    # Convert memory from 2G format to MB
    MEMORY_MB=2048
    if [[ "${LAUNCH_ARGS[1]}" =~ ([0-9]+)G ]]; then
      MEMORY_MB=$((${BASH_REMATCH[1]} * 1024))
    fi
    
    # Create the VM
    log "Creating new UTM VM from Ubuntu 22.04 image"
    utm create \
      --name "$VM_NAME" \
      --cpu "${LAUNCH_ARGS[5]}" \
      --memory "$MEMORY_MB" \
      --arch "x86_64" \
      --disk "20G" \
      --iso "$IMAGE_FILE" || {
      echo "❌ Failed to create UTM VM!"
      echo "Cannot continue without a working VM"
      exit 1
    }
    
    # Start the VM
    utm start "$VM_NAME" || {
      echo "❌ Failed to start UTM VM!"
      echo "Cannot continue without a working VM"
      exit 1
    }
  fi
else
  log "Launching VM with: multipass launch 22.04 --name $VM_NAME ${LAUNCH_ARGS[*]}"
  multipass launch 22.04 --name $VM_NAME "${LAUNCH_ARGS[@]}" || {
    echo "❌ Failed to launch VM!"
    echo "Cannot continue without a working VM"
    exit 1
  }
fi

# Wait for VM to be ready with SSH connectivity
echo "⌛ Waiting for VM to be ready with SSH connectivity..."
for ((i=1; i<=60; i++)); do
  echo -n "."
  if [[ $USE_UTM -eq 1 ]]; then
    # For UTM, we need to get the IP and use SSH
    VM_IP=$(utm ip $VM_NAME 2>/dev/null)
    if [[ -n "$VM_IP" ]]; then
      if ssh -o StrictHostKeyChecking=no -o ConnectTimeout=5 -o UserKnownHostsFile=/dev/null ubuntu@$VM_IP echo "SSH test" &>/dev/null; then
        echo " Connected to $VM_IP!"
        break
      fi
    fi
  else
    if multipass exec $VM_NAME -- echo "SSH test" &>/dev/null; then
      echo " Connected!"
      break
    fi
  fi
  
  if [[ $i -eq 60 ]]; then
    echo " Failed to connect!"
    echo "❌ Error: Could not establish SSH connection to VM"
    if [[ $KEEP_VM -eq 0 ]]; then
      if [[ $USE_UTM -eq 1 ]]; then
        utm stop $VM_NAME
        utm delete $VM_NAME
      else
        multipass delete $VM_NAME --purge
      fi
    fi
    exit 1
  fi
  
  sleep 5
done

# Check VM architecture
if [[ $USE_UTM -eq 1 ]]; then
  VM_IP=$(utm ip $VM_NAME)
  VM_ARCH=$(ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null ubuntu@$VM_IP uname -m)
else
  VM_ARCH=$(multipass exec $VM_NAME -- uname -m)
fi
echo "📊 VM Architecture: $VM_ARCH"

# Verify x86_64 architecture
if [[ "$VM_ARCH" != "x86_64" ]]; then
  echo "⚠️  WARNING: VM is not running with x86_64 architecture!"
  echo "    This may cause compatibility issues with the installer."
fi

# Set up DNS for test domain in the VM's hosts file
echo "📝 Setting up test domain in VM hosts file..."
if [[ $USE_UTM -eq 1 ]]; then
  VM_IP=$(utm ip $VM_NAME)
  ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null ubuntu@$VM_IP sudo bash -c "echo '127.0.0.1 test.example.com' >> /etc/hosts"
else
  multipass exec $VM_NAME -- sudo bash -c "echo '127.0.0.1 test.example.com' >> /etc/hosts"
fi

# Copy binary to VM
echo "📦 Copying binary to VM..."
if [[ $USE_UTM -eq 1 ]]; then
  VM_IP=$(utm ip $VM_NAME)
  scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null bin/infinity-metrics ubuntu@$VM_IP:/home/ubuntu/
  ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null ubuntu@$VM_IP sudo bash -c "chmod +x /home/ubuntu/infinity-metrics && mv /home/ubuntu/infinity-metrics /usr/local/bin/"
else
  multipass transfer bin/infinity-metrics $VM_NAME:/home/ubuntu/
  multipass exec $VM_NAME -- sudo bash -c "chmod +x /home/ubuntu/infinity-metrics && mv /home/ubuntu/infinity-metrics /usr/local/bin/"
fi

# Run the test in the VM
echo "🧪 Running test in VM..."

# Set up appropriate environment variables in VM
if [[ $USE_UTM -eq 1 ]]; then
  VM_IP=$(utm ip $VM_NAME)
  ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null ubuntu@$VM_IP sudo bash -c "echo 'ENV=test' >> /etc/environment"
  ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null ubuntu@$VM_IP sudo bash -c "echo 'SKIP_DNS_VALIDATION=$SKIP_DNS_CHECKS' >> /etc/environment"
  ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null ubuntu@$VM_IP sudo bash -c "echo 'ADMIN_PASSWORD=password123' >> /etc/environment"
  ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null ubuntu@$VM_IP sudo bash -c "echo 'PLATFORM_CHECK_DISABLED=1' >> /etc/environment"
else
  multipass exec $VM_NAME -- sudo bash -c "echo 'ENV=test' >> /etc/environment"
  multipass exec $VM_NAME -- sudo bash -c "echo 'SKIP_DNS_VALIDATION=$SKIP_DNS_CHECKS' >> /etc/environment"
  multipass exec $VM_NAME -- sudo bash -c "echo 'ADMIN_PASSWORD=password123' >> /etc/environment"
  multipass exec $VM_NAME -- sudo bash -c "echo 'PLATFORM_CHECK_DISABLED=1' >> /etc/environment"
fi

# Prepare input for the command
INPUT_FILE=$(mktemp)
cat > "$INPUT_FILE" << EOF
test.example.com
admin@example.com
test-license-key
y
EOF

# Run the installation with stdin input
if [[ $USE_UTM -eq 1 ]]; then
  # For UTM, we need to create the input file in the VM and use SSH
  VM_IP=$(utm ip $VM_NAME)
  scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null "$INPUT_FILE" ubuntu@$VM_IP:/tmp/test_input.txt
  ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null ubuntu@$VM_IP sudo bash -c "cat /tmp/test_input.txt | sudo infinity-metrics install"
  TEST_RESULT=$?
  ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null ubuntu@$VM_IP sudo rm -f /tmp/test_input.txt
else
  cat "$INPUT_FILE" | multipass exec $VM_NAME -- sudo infinity-metrics install
  TEST_RESULT=$?
fi

# Check installation result
if [[ $TEST_RESULT -eq 0 ]]; then
  echo "✅ Tests completed successfully"
else
  echo "❌ Tests failed with exit code: $TEST_RESULT"
fi

# Cleanup
rm -f "$INPUT_FILE"

# Clean up (unless --keep-vm was specified)
if [[ $KEEP_VM -eq 0 ]]; then
  echo "🧹 Cleaning up VM..."
  if [[ $USE_UTM -eq 1 ]]; then
    utm stop $VM_NAME
    utm delete $VM_NAME
  else
    multipass delete $VM_NAME --purge
  fi
else
  echo "🔍 Keeping VM for inspection: $VM_NAME"
  if [[ $USE_UTM -eq 1 ]]; then
    VM_IP=$(utm ip $VM_NAME)
    echo "You can access it with: ssh ubuntu@$VM_IP"
  else
    echo "You can access it with: multipass shell $VM_NAME"
  fi
fi

# Configure test runner environment
export ENV=test
export DEBUG=$DEBUG
export VM_NAME=$VM_NAME
export SKIP_DNS_VALIDATION=$SKIP_DNS_CHECKS

# Export VM provider information
if [[ $USE_UTM -eq 1 ]]; then
  export VM_PROVIDER="utm"
else
  export VM_PROVIDER="multipass"
fi


exit $TEST_RESULT 
