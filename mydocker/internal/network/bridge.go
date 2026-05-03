package network

import (
	"fmt"
	"net"
	"os"
	"os/exec"
)

const (
	BridgeName = "mydocker0"
	BridgeIP   = "172.20.0.1/16"
	Subnet     = "172.20.0.0/16"
)

// SetupBridge tạo bridge mydocker0 nếu chưa có
func SetupBridge() error {
	// Check bridge đã tồn tại chưa
	if _, err := net.InterfaceByName(BridgeName); err == nil {
		return nil // đã có rồi
	}

	// Tạo bridge
	if err := run("ip", "link", "add", BridgeName, "type", "bridge"); err != nil {
		return fmt.Errorf("create bridge: %w", err)
	}

	// Gán IP
	if err := run("ip", "addr", "add", BridgeIP, "dev", BridgeName); err != nil {
		return fmt.Errorf("add bridge IP: %w", err)
	}

	// Bring up
	if err := run("ip", "link", "set", BridgeName, "up"); err != nil {
		return fmt.Errorf("bridge up: %w", err)
	}

	// Enable IP forwarding
	if err := os.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1"), 0644); err != nil {
		return fmt.Errorf("ip_forward: %w", err)
	}

	// iptables NAT MASQUERADE
	// Dùng "! -o mydocker0" để không phụ thuộc tên interface eth0
	if err := run("iptables", "-t", "nat", "-A", "POSTROUTING",
		"-s", Subnet, "!", "-o", BridgeName, "-j", "MASQUERADE"); err != nil {
		// Ignore nếu rule đã tồn tại
		fmt.Fprintf(os.Stderr, "iptables warning: %v\n", err)
	}

	// Allow forwarding
	if err := run("iptables", "-A", "FORWARD",
		"-i", BridgeName, "-j", "ACCEPT"); err != nil {
		fmt.Fprintf(os.Stderr, "iptables forward warning: %v\n", err)
	}
	if err := run("iptables", "-A", "FORWARD",
		"-o", BridgeName, "-j", "ACCEPT"); err != nil {
		fmt.Fprintf(os.Stderr, "iptables forward warning: %v\n", err)
	}

	return nil
}

// TeardownBridge xóa bridge (dùng khi cleanup)
func TeardownBridge() {
	_ = run("ip", "link", "del", BridgeName)
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
