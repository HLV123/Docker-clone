package network

import (
	"fmt"
	"os"
	"os/exec"
)

// AddPortForward adds iptables DNAT rule for port forwarding
// host:hostPort -> containerIP:containerPort
func AddPortForward(hostPort int, containerIP string, containerPort int, protocol string) error {
	if protocol == "" {
		protocol = "tcp"
	}

	// DNAT: incoming traffic on hostPort -> containerIP:containerPort
	if err := iptables("-t", "nat", "-A", "PREROUTING",
		"-p", protocol,
		"--dport", fmt.Sprintf("%d", hostPort),
		"-j", "DNAT",
		"--to-destination", fmt.Sprintf("%s:%d", containerIP, containerPort),
	); err != nil {
		return fmt.Errorf("DNAT rule: %w", err)
	}

	// Also handle localhost access on host
	if err := iptables("-t", "nat", "-A", "OUTPUT",
		"-p", protocol,
		"-d", "127.0.0.1",
		"--dport", fmt.Sprintf("%d", hostPort),
		"-j", "DNAT",
		"--to-destination", fmt.Sprintf("%s:%d", containerIP, containerPort),
	); err != nil {
		// Non-fatal
		fmt.Fprintf(os.Stderr, "OUTPUT DNAT warning: %v\n", err)
	}

	return nil
}

// RemovePortForward removes iptables DNAT rule
func RemovePortForward(hostPort int, protocol string) error {
	if protocol == "" {
		protocol = "tcp"
	}

	// Try to delete — ignore errors (rule may not exist)
	_ = iptables("-t", "nat", "-D", "PREROUTING",
		"-p", protocol,
		"--dport", fmt.Sprintf("%d", hostPort),
		"-j", "DNAT",
	)
	_ = iptables("-t", "nat", "-D", "OUTPUT",
		"-p", protocol,
		"-d", "127.0.0.1",
		"--dport", fmt.Sprintf("%d", hostPort),
		"-j", "DNAT",
	)
	return nil
}

func iptables(args ...string) error {
	cmd := exec.Command("iptables", args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
