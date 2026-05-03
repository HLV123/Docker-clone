package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"text/tabwriter"
	"time"
)

const containersRoot = "/var/lib/mydocker/containers"

type PortMapping struct {
	HostPort      int    `json:"host_port"`
	ContainerPort int    `json:"container_port"`
	Protocol      string `json:"protocol"`
}

type VolumeMount struct {
	HostPath      string `json:"host_path"`
	ContainerPath string `json:"container_path"`
	ReadOnly      bool   `json:"read_only"`
}

type State struct {
	ID        string        `json:"id"`
	Name      string        `json:"name,omitempty"`
	Image     string        `json:"image"`
	Command   []string      `json:"command"`
	PID       int           `json:"pid"`
	Status    string        `json:"status"`
	IP        string        `json:"ip,omitempty"`
	Ports     []PortMapping `json:"ports,omitempty"`
	CreatedAt time.Time     `json:"created_at"`
	ExitedAt  *time.Time    `json:"exited_at,omitempty"`
	ExitCode  *int          `json:"exit_code,omitempty"`
}

// ContainerConfig holds runtime config (volumes, env, hostname)
type ContainerConfig struct {
	Env        []string      `json:"env"`
	Volumes    []VolumeMount `json:"volumes"`
	Hostname   string        `json:"hostname"`
	WorkingDir string        `json:"working_dir,omitempty"`
}

func Save(s *State) error {
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now()
	}
	if s.Status == "exited" {
		now := time.Now()
		s.ExitedAt = &now
	}
	dir := filepath.Join(containersRoot, s.ID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "state.json"), data, 0644)
}

func SaveConfig(containerID string, cfg *ContainerConfig) error {
	dir := filepath.Join(containersRoot, containerID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "config.json"), data, 0644)
}

func LoadConfig(containerID string) (*ContainerConfig, error) {
	path := filepath.Join(containersRoot, containerID, "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return &ContainerConfig{}, nil // optional file
	}
	var cfg ContainerConfig
	return &cfg, json.Unmarshal(data, &cfg)
}

func Load(containerID string) (*State, error) {
	path := filepath.Join(containersRoot, containerID, "state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s State
	return &s, json.Unmarshal(data, &s)
}

func PS(showAll bool) error {
	entries, err := os.ReadDir(containersRoot)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "CONTAINER ID\tNAME\tIMAGE\tCOMMAND\tSTATUS\tPORTS\tIP\tCREATED")

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		s, err := Load(e.Name())
		if err != nil {
			continue
		}
		syncStatus(s)
		if !showAll && s.Status != "running" {
			continue
		}
		cmd := ""
		if len(s.Command) > 0 {
			cmd = s.Command[0]
		}
		name := s.Name
		if name == "" {
			name = "-"
		}
		ports := formatPorts(s.Ports)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			s.ID, name, s.Image, cmd, s.Status, ports, s.IP,
			s.CreatedAt.Format("2006-01-02 15:04:05"),
		)
	}
	return w.Flush()
}

func formatPorts(ports []PortMapping) string {
	if len(ports) == 0 {
		return "-"
	}
	result := ""
	for i, p := range ports {
		if i > 0 {
			result += ", "
		}
		result += fmt.Sprintf("%d->%d/%s", p.HostPort, p.ContainerPort, p.Protocol)
	}
	return result
}

func syncStatus(s *State) {
	if s.Status != "running" {
		return
	}
	if s.PID <= 0 {
		s.Status = "exited"
		_ = Save(s)
		return
	}
	if err := syscall.Kill(s.PID, 0); err != nil {
		s.Status = "exited"
		_ = Save(s)
	}
}

func Remove(containerID string, force bool) error {
	s, err := Load(containerID)
	if err != nil {
		return fmt.Errorf("container not found: %s", containerID)
	}
	syncStatus(s)
	if s.Status == "running" {
		if !force {
			return fmt.Errorf("container %s is running, use -f to force", containerID)
		}
		if s.PID > 0 {
			_ = syscall.Kill(s.PID, syscall.SIGKILL)
		}
	}
	mergedDir := filepath.Join(containersRoot, containerID, "merged")
	_ = syscall.Unmount(mergedDir, syscall.MNT_DETACH)
	return os.RemoveAll(filepath.Join(containersRoot, containerID))
}

func Logs(containerID string) error {
	logPath := filepath.Join(containersRoot, containerID, "container.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no logs for container %s (was it run with -d?)", containerID)
		}
		return err
	}
	fmt.Print(string(data))
	return nil
}

func Stats(containerID string) error {
	s, err := Load(containerID)
	if err != nil {
		return fmt.Errorf("container not found: %s", containerID)
	}
	syncStatus(s)
	if s.Status != "running" {
		return fmt.Errorf("container %s is not running", containerID)
	}

	cgroupPath := fmt.Sprintf("/sys/fs/cgroup/mydocker/%s", containerID)

	memCurrent := readCgroupFile(filepath.Join(cgroupPath, "memory.current"))
	memMax := readCgroupFile(filepath.Join(cgroupPath, "memory.max"))
	cpuUsage := readCgroupFile(filepath.Join(cgroupPath, "cpu.stat"))

	fmt.Printf("Container: %s\n", containerID)
	fmt.Printf("Status:    %s (PID %d)\n", s.Status, s.PID)
	fmt.Printf("Memory:    %s / %s\n", formatBytes(memCurrent), formatBytesOrMax(memMax))
	fmt.Printf("CPU stat:\n%s\n", cpuUsage)
	return nil
}

func readCgroupFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "N/A"
	}
	return string(data)
}

func formatBytes(s string) string {
	var n int64
	fmt.Sscanf(s, "%d", &n)
	switch {
	case n >= 1024*1024*1024:
		return fmt.Sprintf("%.1f GB", float64(n)/1024/1024/1024)
	case n >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(n)/1024/1024)
	case n >= 1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func formatBytesOrMax(s string) string {
	if s == "max\n" || s == "max" {
		return "unlimited"
	}
	return formatBytes(s)
}
