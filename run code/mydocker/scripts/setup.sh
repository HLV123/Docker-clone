#!/bin/bash
# setup.sh — cấu hình hệ thống để chạy mydocker
# Chạy: sudo bash scripts/setup.sh
set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

ok()   { echo -e "${GREEN}[OK]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
fail() { echo -e "${RED}[FAIL]${NC} $1"; exit 1; }
info() { echo -e "  --> $1"; }

echo "========================================"
echo "  mydocker setup"
echo "========================================"
echo ""

# --- 1. Kiểm tra OS ---
if ! grep -qi "ubuntu" /etc/os-release 2>/dev/null; then
    warn "Not Ubuntu — some steps may differ"
fi

UBUNTU_VER=$(grep VERSION_ID /etc/os-release 2>/dev/null | cut -d'"' -f2)
info "OS: Ubuntu $UBUNTU_VER"

# --- 2. Kiểm tra WSL2 ---
IS_WSL=false
if grep -qi "microsoft" /proc/version 2>/dev/null; then
    IS_WSL=true
    info "Running inside WSL2"
fi

# --- 3. Kiểm tra kernel ---
KERNEL=$(uname -r)
info "Kernel: $KERNEL"

# --- 4. Cài dependencies ---
echo ""
echo "[1/7] Installing system dependencies..."
apt-get update -qq
apt-get install -y -qq \
    build-essential git iproute2 iptables \
    ca-certificates uidmap nsenter runc \
    2>/dev/null || warn "Some packages failed to install"
ok "Dependencies installed"

# --- 5. Kiểm tra cgroup v2 ---
echo ""
echo "[2/7] Checking cgroup v2..."
if mount | grep -q "cgroup2 on /sys/fs/cgroup "; then
    ok "cgroup v2 unified hierarchy"
else
    fail "cgroup v2 not found. Enable in /etc/wsl.conf: [boot] systemd=true, then restart WSL"
fi

# Enable controllers
echo "+memory +cpu +pids" > /sys/fs/cgroup/cgroup.subtree_control 2>/dev/null || true
mkdir -p /sys/fs/cgroup/mydocker 2>/dev/null || true
echo "+memory +cpu +pids" > /sys/fs/cgroup/mydocker/cgroup.subtree_control 2>/dev/null || true
ok "cgroup controllers enabled"

# --- 6. Kiểm tra overlayfs ---
echo ""
echo "[3/7] Checking overlayfs..."
if grep -q overlay /proc/filesystems; then
    ok "overlayfs available"
else
    fail "overlayfs not available"
fi

# --- 7. Setup network bridge ---
echo ""
echo "[4/7] Setting up network bridge..."
if ! ip link show mydocker0 &>/dev/null; then
    ip link add mydocker0 type bridge 2>/dev/null || true
    ip addr add 172.20.0.1/16 dev mydocker0 2>/dev/null || true
    ip link set mydocker0 up 2>/dev/null || true
    info "Bridge mydocker0 created (172.20.0.1/16)"
else
    info "Bridge mydocker0 already exists"
fi

# Enable IP forwarding
echo 1 > /proc/sys/net/ipv4/ip_forward

# iptables NAT
iptables -t nat -C POSTROUTING -s 172.20.0.0/16 ! -o mydocker0 -j MASQUERADE 2>/dev/null || \
    iptables -t nat -A POSTROUTING -s 172.20.0.0/16 ! -o mydocker0 -j MASQUERADE 2>/dev/null || true
iptables -C FORWARD -i mydocker0 -j ACCEPT 2>/dev/null || \
    iptables -A FORWARD -i mydocker0 -j ACCEPT 2>/dev/null || true
iptables -C FORWARD -o mydocker0 -j ACCEPT 2>/dev/null || \
    iptables -A FORWARD -o mydocker0 -j ACCEPT 2>/dev/null || true
ok "Network bridge configured"

# --- 8. Setup runtime dirs ---
echo ""
echo "[5/7] Creating runtime directories..."
mkdir -p /var/lib/mydocker/{images,containers,dns,volumes}
chmod 755 /var/lib/mydocker
ok "Runtime dirs: /var/lib/mydocker/"

# --- 9. Setup WSL2 systemd (nếu chưa có) ---
echo ""
echo "[6/7] WSL2 configuration..."
if [ "$IS_WSL" = true ]; then
    WSL_CONF=/etc/wsl.conf
    if ! grep -q "systemd=true" $WSL_CONF 2>/dev/null; then
        cat >> $WSL_CONF << 'EOF'

[boot]
systemd=true
EOF
        warn "Added systemd=true to /etc/wsl.conf — restart WSL to apply: wsl --shutdown"
    else
        ok "/etc/wsl.conf already configured"
    fi

    # Cgroup delegation
    mkdir -p /etc/systemd/system/user@.service.d
    cat > /etc/systemd/system/user@.service.d/delegate.conf << 'EOF'
[Service]
Delegate=cpu cpuset io memory pids
EOF
    systemctl daemon-reexec 2>/dev/null || true
    ok "cgroup delegation configured"
else
    ok "Not WSL2, skipping WSL config"
fi

# --- 10. Persist network config ---
echo ""
echo "[7/7] Persisting network config..."
PERSIST_SCRIPT=/etc/mydocker-network-setup.sh
cat > $PERSIST_SCRIPT << 'NETEOF'
#!/bin/bash
# Auto-run on boot to restore mydocker network
ip link show mydocker0 &>/dev/null || {
    ip link add mydocker0 type bridge
    ip addr add 172.20.0.1/16 dev mydocker0
    ip link set mydocker0 up
}
echo 1 > /proc/sys/net/ipv4/ip_forward
echo "+memory +cpu +pids" > /sys/fs/cgroup/cgroup.subtree_control 2>/dev/null || true
mkdir -p /sys/fs/cgroup/mydocker
echo "+memory +cpu +pids" > /sys/fs/cgroup/mydocker/cgroup.subtree_control 2>/dev/null || true
iptables -t nat -C POSTROUTING -s 172.20.0.0/16 ! -o mydocker0 -j MASQUERADE 2>/dev/null || \
    iptables -t nat -A POSTROUTING -s 172.20.0.0/16 ! -o mydocker0 -j MASQUERADE 2>/dev/null || true
NETEOF
chmod +x $PERSIST_SCRIPT

# Thêm vào rc.local nếu có
if [ -f /etc/rc.local ]; then
    grep -q "mydocker-network" /etc/rc.local || \
        sed -i 's|^exit 0|bash /etc/mydocker-network-setup.sh\nexit 0|' /etc/rc.local
fi

# Hoặc tạo systemd service
cat > /etc/systemd/system/mydocker-network.service << 'SVCEOF'
[Unit]
Description=mydocker network setup
After=network.target

[Service]
Type=oneshot
ExecStart=/bin/bash /etc/mydocker-network-setup.sh
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
SVCEOF
systemctl enable mydocker-network.service 2>/dev/null || true
ok "Network config will persist across reboots"

# --- Summary ---
echo ""
echo "========================================"
echo -e "${GREEN}Setup complete!${NC}"
echo "========================================"
echo ""
echo "Quick start:"
echo "  mydocker pull alpine:latest"
echo "  mydocker run -it alpine:latest"
echo "  mydocker run -d -p 8080:80 nginx:latest"
echo ""
echo "Start daemon (for restart policy, background jobs):"
echo "  mydockerd &"
echo ""
echo "docker-compose:"
echo "  cd example && mydocker-compose up -d"
echo ""
if [ "$IS_WSL" = true ] && ! systemctl is-active --quiet systemd 2>/dev/null; then
    echo -e "${YELLOW}NOTE: Restart WSL for full systemd support:${NC}"
    echo "  (in PowerShell) wsl --shutdown"
    echo ""
fi
