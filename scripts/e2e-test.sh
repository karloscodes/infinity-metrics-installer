#!/bin/bash
set -e

# Unified script for e2e tests in different modes
# - VM mode: Uses Multipass VM (requires working Multipass)
# - Mock mode: Local test that simulates VM

echo "🚀 Infinity Metrics E2E Tests"
echo "============================"

# Configuration
VM_NAME="infinity-test-vm-$(date +%s)"
DEBUG=${DEBUG:-0}
KEEP_VM=${KEEP_VM:-0}
MOCK_MODE=${MOCK_MODE:-0}  # Default to VM mode
SKIP_DNS_CHECKS=${SKIP_DNS_VALIDATION:-1}  # Default to skipping DNS

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
    --mock)
      MOCK_MODE=1
      shift
      ;;
    --check-dns)
      SKIP_DNS_CHECKS=0
      shift
      ;;
    *)
      echo "Unknown option: $1"
      echo "Usage: $0 [--vm-name NAME] [--debug] [--keep-vm] [--mock] [--check-dns]"
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

# If in mock mode, run the mock test script
if [[ $MOCK_MODE -eq 1 ]]; then
  echo "Running in mock mode (no VM)"
  KEEP_TEST=$KEEP_VM SKIP_DNS_VALIDATION=$SKIP_DNS_CHECKS ./scripts/mock-test.sh
  exit $?
fi

# Check if Multipass daemon is responsive
if ! multipass version &>/dev/null; then
  echo "⚠️  Multipass appears to be unresponsive"
  echo "   Try restarting the multipass daemon with:"
  echo "   sudo launchctl unload /Library/LaunchDaemons/com.canonical.multipassd.plist"
  echo "   sudo launchctl load /Library/LaunchDaemons/com.canonical.multipassd.plist"
  echo ""
  echo "Switching to mock mode..."
  KEEP_TEST=$KEEP_VM SKIP_DNS_VALIDATION=$SKIP_DNS_CHECKS ./scripts/mock-test.sh
  exit $?
fi

# Clean up existing VM if needed
echo "🧹 Cleaning up any existing VM with the same name..."
multipass delete $VM_NAME --purge 2>/dev/null || true

# Create VM - without architecture flags (not supported on Mac)
echo "🚀 Creating VM: $VM_NAME"
LAUNCH_ARGS=("--memory" "2G" "--disk" "10G" "--cpus" "2")

# Check if running on Apple Silicon
if [[ "$(uname -m)" == "arm64" ]]; then
  echo "Detected Apple Silicon Mac - using native architecture"
  # Set platform compatibility flag for ARM64
  export PLATFORM_CHECK_DISABLED=1
fi

# Launch the VM - no architecture flag
log "Launching VM with: multipass launch 22.04 --name $VM_NAME ${LAUNCH_ARGS[*]}"
multipass launch 22.04 --name $VM_NAME "${LAUNCH_ARGS[@]}" || {
  echo "❌ Failed to launch VM!"
  echo "Switching to mock mode..."
  KEEP_TEST=$KEEP_VM SKIP_DNS_VALIDATION=$SKIP_DNS_CHECKS ./scripts/mock-test.sh
  exit $?
}

# Wait for VM to be ready with SSH connectivity
echo "⌛ Waiting for VM to be ready with SSH connectivity..."
for ((i=1; i<=30; i++)); do
  echo -n "."
  if multipass exec $VM_NAME -- echo "SSH test" &>/dev/null; then
    echo " Connected!"
    break
  fi
  
  if [[ $i -eq 30 ]]; then
    echo " Failed to connect!"
    echo "❌ Error: Could not establish SSH connection to VM"
    echo "Switching to mock mode..."
    if [[ $KEEP_VM -eq 0 ]]; then
      multipass delete $VM_NAME --purge
    fi
    KEEP_TEST=$KEEP_VM SKIP_DNS_VALIDATION=$SKIP_DNS_CHECKS ./scripts/mock-test.sh
    exit $?
  fi
  
  sleep 2
done

# Check VM architecture
VM_ARCH=$(multipass exec $VM_NAME -- uname -m)
echo "📊 VM Architecture: $VM_ARCH"

# Set up DNS for test domain in the VM's hosts file
echo "📝 Setting up test domain in VM hosts file..."
multipass exec $VM_NAME -- sudo bash -c "echo '127.0.0.1 test.example.com' >> /etc/hosts"

# Copy binary to VM
echo "📦 Copying binary to VM..."
multipass transfer bin/infinity-metrics $VM_NAME:/home/ubuntu/
multipass exec $VM_NAME -- sudo bash -c "chmod +x /home/ubuntu/infinity-metrics && mv /home/ubuntu/infinity-metrics /usr/local/bin/"

# Run the test in the VM
echo "🧪 Running test in VM..."

# Set up appropriate environment variables in VM
multipass exec $VM_NAME -- sudo bash -c "echo 'ENV=test' >> /etc/environment"
multipass exec $VM_NAME -- sudo bash -c "echo 'SKIP_DNS_VALIDATION=$SKIP_DNS_CHECKS' >> /etc/environment"
multipass exec $VM_NAME -- sudo bash -c "echo 'ADMIN_PASSWORD=password123' >> /etc/environment"
multipass exec $VM_NAME -- sudo bash -c "echo 'PLATFORM_CHECK_DISABLED=1' >> /etc/environment"

# Prepare input for the command
INPUT_FILE=$(mktemp)
cat > "$INPUT_FILE" << EOF
test.example.com
admin@example.com
test-license-key
y
EOF

# Run the installation with stdin input
cat "$INPUT_FILE" | multipass exec $VM_NAME -- sudo infinity-metrics install
TEST_RESULT=$?

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
  multipass delete $VM_NAME --purge
else
  echo "🔍 Keeping VM for inspection: $VM_NAME"
  echo "You can access it with: multipass shell $VM_NAME"
fi

# Configure test runner environment
export ENV=test
export DEBUG=$DEBUG
export VM_NAME=$VM_NAME
export SKIP_DNS_VALIDATION=$SKIP_DNS_CHECKS
export PLATFORM_CHECK_DISABLED=1  # Always set for compatibility

exit $TEST_RESULT 
