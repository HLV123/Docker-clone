package network

import (
	"fmt"
	"math/rand"
	"os/exec"
	"strings"
)

// VethPair đại diện cho một veth pair
type VethPair struct {
	HostVeth      string // gắn vào bridge (host side)
	ContainerVeth string // đưa vào container namespace
	ContainerIP   string // IP gán cho container
}

// NewVethPair tạo tên veth pair dựa trên containerID
func NewVethPair(containerID string) *VethPair {
	suffix := containerID[:6]
	return &VethPair{
		HostVeth:      "veth-h-" + suffix,
		ContainerVeth: "veth-c-" + suffix,
		ContainerIP:   allocateIP(),
	}
}

// Create tạo veth pair và gắn host side vào bridge
func (v *VethPair) Create() error {
	// Tạo veth pair
	if err := run("ip", "link", "add", v.HostVeth,
		"type", "veth", "peer", "name", v.ContainerVeth); err != nil {
		return fmt.Errorf("create veth pair: %w", err)
	}

	// Gắn host side vào bridge
	if err := run("ip", "link", "set", v.HostVeth, "master", BridgeName); err != nil {
		return fmt.Errorf("attach to bridge: %w", err)
	}

	// Bring up host side
	if err := run("ip", "link", "set", v.HostVeth, "up"); err != nil {
		return fmt.Errorf("veth host up: %w", err)
	}

	return nil
}

// MoveToNetns di chuyển container side vào network namespace của container
func (v *VethPair) MoveToNetns(pid int) error {
	return run("ip", "link", "set", v.ContainerVeth, "netns", fmt.Sprintf("%d", pid))
}

// SetupInsideContainer cấu hình IP và route bên trong container
// Chạy từ host, dùng "ip netns exec" trick hoặc nsenter
func (v *VethPair) SetupInsideContainer(pid int) error {
	nsenter := fmt.Sprintf("/proc/%d/ns/net", pid)

	cmds := [][]string{
		{"ip", "link", "set", "lo", "up"},
		{"ip", "link", "set", v.ContainerVeth, "up"},
		{"ip", "addr", "add", v.ContainerIP + "/16", "dev", v.ContainerVeth},
		{"ip", "route", "add", "default", "via", "172.20.0.1"},
	}

	for _, args := range cmds {
		cmd := exec.Command("nsenter", append([]string{
			"--net=" + nsenter, "--",
		}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("nsenter %v: %w (output: %s)", args, err, strings.TrimSpace(string(out)))
		}
	}

	return nil
}

// Cleanup xóa veth pair (host side — container side tự xóa khi namespace destroy)
func (v *VethPair) Cleanup() {
	_ = run("ip", "link", "del", v.HostVeth)
}

// allocateIP chọn IP ngẫu nhiên trong dải 172.20.1.0 - 172.20.254.254
func allocateIP() string {
	third := rand.Intn(254) + 1
	fourth := rand.Intn(254) + 1
	return fmt.Sprintf("172.20.%d.%d", third, fourth)
}
