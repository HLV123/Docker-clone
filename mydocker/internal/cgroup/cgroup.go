package cgroup

import (
	"fmt"
	"os"
	"path/filepath"
)

const cgroupRoot = "/sys/fs/cgroup/mydocker"

// Cgroup đại diện cho một container cgroup v2
type Cgroup struct {
	containerID string
	path        string
}

func New(containerID string) *Cgroup {
	return &Cgroup{
		containerID: containerID,
		path:        filepath.Join(cgroupRoot, containerID),
	}
}

// Create tạo cgroup directory và enable controllers
func (c *Cgroup) Create() error {
	// Tạo parent mydocker nếu chưa có
	if err := os.MkdirAll(cgroupRoot, 0755); err != nil {
		return fmt.Errorf("mkdir cgroup root: %w", err)
	}

	// Enable controllers cho subtree của parent
	parentSubtree := filepath.Join(cgroupRoot, "cgroup.subtree_control")
	controllers := "+memory +cpu +pids"
	// Ignore error nếu controller chưa available
	_ = os.WriteFile(parentSubtree, []byte(controllers), 0644)

	// Tạo cgroup cho container này
	if err := os.MkdirAll(c.path, 0755); err != nil {
		return fmt.Errorf("mkdir cgroup: %w", err)
	}

	return nil
}

// SetLimits set memory, CPU, pids limits
func (c *Cgroup) SetLimits(memBytes int64, cpuCores float64, pids int) error {
	if memBytes > 0 {
		if err := os.WriteFile(
			filepath.Join(c.path, "memory.max"),
			[]byte(fmt.Sprintf("%d", memBytes)),
			0644,
		); err != nil {
			return fmt.Errorf("set memory.max: %w", err)
		}
		// Disable swap
		_ = os.WriteFile(
			filepath.Join(c.path, "memory.swap.max"),
			[]byte("0"),
			0644,
		)
	}

	if cpuCores > 0 {
		// cpu.max format: "quota period"
		// quota = cpuCores * period
		period := int64(100000)
		quota := int64(cpuCores * float64(period))
		if err := os.WriteFile(
			filepath.Join(c.path, "cpu.max"),
			[]byte(fmt.Sprintf("%d %d", quota, period)),
			0644,
		); err != nil {
			return fmt.Errorf("set cpu.max: %w", err)
		}
	}

	if pids > 0 {
		if err := os.WriteFile(
			filepath.Join(c.path, "pids.max"),
			[]byte(fmt.Sprintf("%d", pids)),
			0644,
		); err != nil {
			return fmt.Errorf("set pids.max: %w", err)
		}
	}

	return nil
}

// AddProcess ghi PID vào cgroup.procs
func (c *Cgroup) AddProcess(pid int) error {
	return os.WriteFile(
		filepath.Join(c.path, "cgroup.procs"),
		[]byte(fmt.Sprintf("%d", pid)),
		0644,
	)
}

// Destroy xóa cgroup sau khi container exit
func (c *Cgroup) Destroy() {
	// Đảm bảo không còn process nào
	_ = os.Remove(c.path)
}
