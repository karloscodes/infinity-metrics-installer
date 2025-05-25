#!/bin/bash
set -e

# Advanced script to diagnose and fix Multipass VM networking issues on M4 Macs
VM_NAME=${1:-"infinity-test-vm"}

echo "🔍 Deep Multipass VM Diagnostics - For Mac M4 (Apple Silicon) 🔍"
echo "=============================================================="
echo "Testing VM: $VM_NAME"

# Get Multipass version and service details
echo -e "\n📋 Multipass Environment:"
echo "-------------------------"
echo "Multipass version:"
multipass version
echo

echo "Multipass daemon status:"
ps -ef | grep multipassd | grep -v grep
echo

# Check if VM exists
if ! multipass list | grep -q "$VM_NAME"; then
    echo "❌ Error: VM $VM_NAME does not exist!"
    exit 1
fi

# Check VM state
echo -e "\n📊 VM State and Details:"
echo "-------------------------"
echo "VM list:"
multipass list
echo

echo "VM info:"
multipass info $VM_NAME --format yaml
echo

# Check qemu process for the VM
echo -e "\n🔄 QEMU Process Check:"
echo "-------------------------"
PS_OUTPUT=$(ps -ef | grep qemu | grep -v grep)
echo "$PS_OUTPUT"
echo

# Check VM architecture and network details inside VM
echo -e "\n🧪 Attempting SSH Connection Test:"
echo "-------------------------"
echo "1. Basic SSH connectivity test:"
multipass exec $VM_NAME -- echo "SSH test" 2>/dev/null && echo "✅ Basic SSH works" || echo "❌ Basic SSH failed"

# Manually try to connect with ssh directly (without multipass exec)
echo -e "\n2. Direct SSH connection test:"
VM_IP=$(multipass info $VM_NAME | grep IPv4 | awk '{print $2}')
if [ -n "$VM_IP" ]; then
    echo "VM IP: $VM_IP"
    echo "Testing direct SSH connection (will timeout after 5 seconds):"
    ssh -o ConnectTimeout=5 -o StrictHostKeyChecking=no ubuntu@$VM_IP echo "Direct SSH test" 2>/dev/null && echo "✅ Direct SSH works" || echo "❌ Direct SSH failed"
else
    echo "❌ Could not determine VM IP address"
fi

# Restart VM as a troubleshooting step
echo -e "\n🔄 Attempting VM Restart:"
echo "-------------------------"
echo "Stopping VM..."
multipass stop $VM_NAME
sleep 2
echo "Starting VM..."
multipass start $VM_NAME
sleep 10
echo "VM state after restart:"
multipass info $VM_NAME | grep State
echo

# Try SSH again after restart
echo -e "\n🧪 Retrying SSH Connection Test After Restart:"
echo "-------------------------"
multipass exec $VM_NAME -- echo "SSH test after restart" 2>/dev/null && echo "✅ SSH works after restart" || echo "❌ SSH still failing after restart"

# Delete and recreate VM with alternative settings if still failing
echo -e "\n⚠️  If SSH is still failing, consider the following options:"
echo "1. Recreate VM with explicit architecture: ./scripts/vm-test.sh --use-arch-flag"
echo "2. Check if Rosetta is properly installed: softwareupdate --install-rosetta"
echo "3. Restart the Multipass daemon:"
echo "   sudo launchctl unload /Library/LaunchDaemons/com.canonical.multipassd.plist"
echo "   sudo launchctl load /Library/LaunchDaemons/com.canonical.multipassd.plist"
echo "4. Update Multipass to the latest version"
echo "5. Try using lima-vm as an alternative to Multipass: https://github.com/lima-vm/lima"

# Check network connectivity from host to VM
echo -e "\n🔄 Network Connectivity Test:"
echo "-------------------------"
if [ -n "$VM_IP" ]; then
    echo "Ping test to VM ($VM_IP):"
    ping -c 3 $VM_IP && echo "✅ Ping works" || echo "❌ Ping failed"
    
    echo "Traceroute to VM:"
    traceroute -m 5 $VM_IP 2>/dev/null || echo "❌ Traceroute failed or not available"
else
    echo "❌ Cannot test network connectivity without VM IP"
fi

echo -e "\n🧩 System Information:"
echo "-------------------------"
echo "macOS Version:"
sw_vers
echo

echo "Kernel Version:"
uname -a
echo

echo "RAM and CPU:"
system_profiler SPHardwareDataType | grep -E "Memory|Cores"
echo

echo "Hypervisor Framework:"
hyperkit -v 2>/dev/null || echo "HyperKit not found or not in PATH"
echo

echo "Rosetta Status:"
pgrep -l oahd >/dev/null && echo "✅ Rosetta is running" || echo "❌ Rosetta is not running"
arch -x86_64 true 2>/dev/null && echo "✅ Rosetta can execute x86_64 binaries" || echo "❌ Rosetta cannot execute x86_64 binaries"
echo

echo "=============================================================="
echo "🏁 Diagnostic complete. Check the output above for issues." 
