#!/bin/bash
# quick-start.sh — Build + install + setup trong 1 lệnh
# Chạy: sudo bash quick-start.sh
set -e

echo "=== mydocker quick start ==="
echo ""

# Kiểm tra Go
if ! command -v go &>/dev/null; then
    echo "Go chưa được cài. Cài Go trước:"
    echo "  snap install go --classic"
    echo "  hoặc: https://go.dev/dl/"
    exit 1
fi

GO_VER=$(go version | awk '{print $3}' | sed 's/go//')
echo "Go $GO_VER detected"

# Build
echo "Building..."
make build

# Install
echo "Installing..."
make install

# Setup system
echo "Setting up system..."
sudo bash scripts/setup.sh

echo ""
echo "=== Done! Try: ==="
echo "  mydocker run -it alpine:latest"
