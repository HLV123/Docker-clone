#!/bin/bash
# Setup mydocker rootless — run once, then use mydocker without sudo
set -e
USER=$(whoami)
echo "Setting up rootless mydocker for $USER..."

# Install uidmap tools
sudo apt-get install -y uidmap 2>/dev/null || true

# Setup subuid/subgid for user namespace
grep -q "^$USER:" /etc/subuid 2>/dev/null || echo "$USER:100000:65536" | sudo tee -a /etc/subuid
grep -q "^$USER:" /etc/subgid 2>/dev/null || echo "$USER:100000:65536" | sudo tee -a /etc/subgid

# Create and own mydocker dirs
sudo mkdir -p /var/lib/mydocker/{images,containers,dns}
sudo chown -R $(id -u):$(id -g) /var/lib/mydocker

# Cgroup delegation for non-root
sudo mkdir -p /etc/systemd/system/user@.service.d
printf "[Service]\nDelegate=cpu cpuset io memory pids\n" | \
    sudo tee /etc/systemd/system/user@.service.d/delegate.conf > /dev/null
sudo systemctl daemon-reexec 2>/dev/null || true

# Install binary with capabilities
sudo cp ./mydocker /usr/local/bin/mydocker
sudo setcap cap_net_admin,cap_net_raw,cap_sys_admin+ep /usr/local/bin/mydocker 2>/dev/null && \
    echo "Capabilities set — network works without sudo" || \
    echo "Note: network/cgroup features still need sudo"

echo ""
echo "Done! You can now run:"
echo "  mydocker run alpine:latest /bin/sh"
echo "  mydocker run -it alpine:latest"
