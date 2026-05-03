package container

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"mydocker/internal/cgroup"
	"mydocker/internal/image"
	"encoding/json"
	"mydocker/internal/network"
	"mydocker/internal/state"

	"github.com/google/uuid"
)

const MyDockerRoot = "/var/lib/mydocker"

type PortMapping struct {
	HostPort      int
	ContainerPort int
	Protocol      string
}

type VolumeMount struct {
	HostPath      string
	ContainerPath string
	ReadOnly      bool
}

type Config struct {
	Image       string
	Command     []string
	Entrypoint  []string
	MemoryLimit int64
	CPULimit    float64
	PidsLimit   int
	Detach      bool
	Network     bool
	TTY         bool
	Interactive bool
	Ports       []PortMapping
	Volumes     []VolumeMount
	Env         []string
	Hostname    string
	Name        string
	WorkingDir  string
}

func Run(cfg *Config) {
	// Resolve command from image config if not provided
	resolveCommand(cfg)

	containerID := generateID()
	containerDir := filepath.Join(MyDockerRoot, "containers", containerID)
	if err := os.MkdirAll(containerDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir container dir: %v\n", err)
		os.Exit(1)
	}

	s := &state.State{
		ID:      containerID,
		Image:   cfg.Image,
		Command: cfg.Command,
		Status:  "created",
		Name:    cfg.Name,
	}
	_ = state.Save(s)
        // Generate OCI bundle
        bundleDir := filepath.Join(MyDockerRoot, "containers", containerID, "bundle")
        if err := os.MkdirAll(bundleDir, 0755); err != nil { fmt.Fprintf(os.Stderr, "bundle mkdir err: %v\n", err) }
        if err := os.WriteFile(filepath.Join(bundleDir, "config.json"), buildOCISpec(containerID, cfg), 0644); err != nil { fmt.Fprintf(os.Stderr, "bundle write err: %v\n", err) } else { fmt.Fprintf(os.Stderr, "bundle OK: %s\n", bundleDir); os.WriteFile("/tmp/bundle_debug.txt", []byte(bundleDir), 0644) }
	var veth *network.VethPair
	cloneflags := uintptr(syscall.CLONE_NEWUTS |
		syscall.CLONE_NEWPID |
		syscall.CLONE_NEWNS |
		syscall.CLONE_NEWIPC)

	// Rootless: add user namespace
	if IsRootless() {
		cloneflags = RootlessCloneFlags(cloneflags)
		if err := SetupRootlessDirs(); err != nil {
			fmt.Fprintf(os.Stderr, "rootless setup: %v\n", err)
		}
	}

	if cfg.Network {
		if err := network.SetupBridge(); err != nil {
			fmt.Fprintf(os.Stderr, "setup bridge: %v\n", err)
		}
		veth = network.NewVethPair(containerID)
		if err := veth.Create(); err != nil {
			fmt.Fprintf(os.Stderr, "create veth: %v\n", err)
		}
		cloneflags |= uintptr(syscall.CLONE_NEWNET)
	}

	cg := cgroup.New(containerID)
	if err := cg.Create(); err != nil {
		fmt.Fprintf(os.Stderr, "cgroup create: %v\n", err)
	}
	if err := cg.SetLimits(cfg.MemoryLimit, cfg.CPULimit, cfg.PidsLimit); err != nil {
		fmt.Fprintf(os.Stderr, "cgroup set limits: %v\n", err)
	}

	if err := state.SaveConfig(containerID, cfg.toStateConfig()); err != nil {
		fmt.Fprintf(os.Stderr, "save config: %v\n", err)
	}

	netFlag := "0"
	if cfg.Network {
		netFlag = "1"
	}
	params := append([]string{"init", containerID, cfg.Image, netFlag}, cfg.Command...)
	// Use mydocker.real for reexec to ensure correct binary
	mydockerBin := "/proc/self/exe"
	if _, err := os.Stat("/usr/local/bin/mydocker.real"); err == nil {
		mydockerBin = "/usr/local/bin/mydocker.real"
	}
	cmd := exec.Command(mydockerBin, params...)

	// Build env: base + image env + user env
	baseEnv := []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/root",
		"TERM=xterm",
	}
	cmd.Env = append(baseEnv, cfg.Env...)

	cmd.SysProcAttr = &syscall.SysProcAttr{Cloneflags: cloneflags}

	if cfg.Detach {
		logPath := filepath.Join(containerDir, "container.log")
		logFile, err := os.Create(logPath)
		if err == nil {
			cmd.Stdout = logFile
			cmd.Stderr = logFile
			defer logFile.Close()
		}
		cmd.Stdin = nil
	} else {
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "start container: %v\n", err)
		cg.Destroy()
		if veth != nil {
			veth.Cleanup()
		}
		os.Exit(1)
	}

	_ = cg.AddProcess(cmd.Process.Pid)

	// Rootless: setup UID/GID mapping
	if IsRootless() {
		if err := SetupUserNamespace(cmd.Process.Pid); err != nil {
			fmt.Fprintf(os.Stderr, "user namespace setup: %v\n", err)
		}
	}

	// Wait a bit for namespace to be ready before network setup
	time.Sleep(100 * time.Millisecond)

	if cfg.Network && veth != nil {
		if err := veth.MoveToNetns(cmd.Process.Pid); err != nil {
			fmt.Fprintf(os.Stderr, "move veth to netns: %v\n", err)
		} else {
			if err := veth.SetupInsideContainer(cmd.Process.Pid); err != nil {
				fmt.Fprintf(os.Stderr, "setup network in container: %v\n", err)
			}
		}
		for _, p := range cfg.Ports {
			if err := network.AddPortForward(p.HostPort, veth.ContainerIP, p.ContainerPort, p.Protocol); err != nil {
				fmt.Fprintf(os.Stderr, "port forward %d->%d: %v\n", p.HostPort, p.ContainerPort, err)
			}
		}
	}

	s.PID = cmd.Process.Pid
	s.Status = "running"
	if veth != nil {
		s.IP = veth.ContainerIP
		// Register container name in DNS
		if cfg.Name != "" {
			_ = network.RegisterContainer(cfg.Name, containerID, veth.ContainerIP)
		}
		// Also register by short container ID
		_ = network.RegisterContainer(containerID[:6], containerID, veth.ContainerIP)
	}
	s.Ports = portMappingsToState(cfg.Ports)
	_ = state.Save(s)

	if cfg.Detach {
		fmt.Printf("%s\n", containerID)
		return
	}

	err := cmd.Wait()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	for _, p := range cfg.Ports {
		_ = network.RemovePortForward(p.HostPort, p.Protocol)
	}
	cg.Destroy()
	if veth != nil {
		veth.Cleanup()
		network.UnregisterContainer(containerID)
	}
	s.Status = "exited"
	s.ExitCode = &exitCode
	_ = state.Save(s)
}

// Stop sends SIGTERM, waits 10s, then SIGKILL
func Stop(containerID string) error {
	s, err := state.Load(containerID)
	if err != nil {
		return fmt.Errorf("container not found: %s", containerID)
	}
	if s.Status != "running" {
		return fmt.Errorf("container %s is not running", containerID)
	}
	if s.PID <= 0 {
		return fmt.Errorf("invalid PID")
	}

	fmt.Printf("Stopping container %s...\n", containerID)
	_ = syscall.Kill(s.PID, syscall.SIGTERM)

	// Wait up to 10s for graceful shutdown
	for i := 0; i < 100; i++ {
		time.Sleep(100 * time.Millisecond)
		if err := syscall.Kill(s.PID, 0); err != nil {
			// Process exited
			s.Status = "exited"
			_ = state.Save(s)
			fmt.Printf("Container %s stopped\n", containerID)
			return nil
		}
	}

	// Force kill
	_ = syscall.Kill(s.PID, syscall.SIGKILL)
	s.Status = "exited"
	_ = state.Save(s)
	fmt.Printf("Container %s killed\n", containerID)
	return nil
}

// resolveCommand fills in Command from image config if not specified
func resolveCommand(cfg *Config) {
	if len(cfg.Command) > 0 {
		return // user specified command, use it
	}

	imgCfg, err := image.LoadImageConfig(cfg.Image)
	if err != nil {
		// No config available, use default shell
		cfg.Command = []string{"/bin/sh"}
		cfg.Interactive = true
		cfg.TTY = true
		return
	}

	// Build command: entrypoint + cmd
	entrypoint := cfg.Entrypoint
	if len(entrypoint) == 0 {
		entrypoint = imgCfg.Entrypoint
	}
	cmd := imgCfg.Cmd

	if len(entrypoint) > 0 {
		cfg.Command = append(entrypoint, cmd...)
	} else if len(cmd) > 0 {
		cfg.Command = cmd
	} else {
		cfg.Command = []string{"/bin/sh"}
		cfg.Interactive = true
		cfg.TTY = true
	}

	// Apply image env (user -e overrides)
	if len(imgCfg.Env) > 0 {
		cfg.Env = append(imgCfg.Env, cfg.Env...)
	}

	// Apply WorkingDir from image if not set
	if cfg.WorkingDir == "" {
		cfg.WorkingDir = imgCfg.WorkingDir
	}
}

func generateID() string {
	return uuid.New().String()[:12]
}

func (cfg *Config) toStateConfig() *state.ContainerConfig {
	volumes := []state.VolumeMount{}
	for _, v := range cfg.Volumes {
		volumes = append(volumes, state.VolumeMount{
			HostPath:      v.HostPath,
			ContainerPath: v.ContainerPath,
			ReadOnly:      v.ReadOnly,
		})
	}
	hostname := cfg.Hostname
	if hostname == "" {
		hostname = "container"
	}
	return &state.ContainerConfig{
		Env:        cfg.Env,
		Volumes:    volumes,
		Hostname:   hostname,
		WorkingDir: cfg.WorkingDir,
	}
}

func portMappingsToState(ports []PortMapping) []state.PortMapping {
	result := []state.PortMapping{}
	for _, p := range ports {
		result = append(result, state.PortMapping{
			HostPort:      p.HostPort,
			ContainerPort: p.ContainerPort,
			Protocol:      p.Protocol,
		})
	}
	return result
}

// buildOCISpec generates a minimal OCI config.json for the container
func buildOCISpec(containerID string, cfg *Config) []byte {
	hostname := cfg.Hostname
	if hostname == "" {
		hostname = "container-" + containerID[:6]
	}
	args := cfg.Command
	if len(args) == 0 {
		args = []string{"/bin/sh"}
	}
	cwd := cfg.WorkingDir
	if cwd == "" {
		cwd = "/"
	}
	env := []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/root", "TERM=xterm",
	}
	env = append(env, cfg.Env...)

	spec := map[string]interface{}{
		"ociVersion": "1.0.2",
		"hostname":   hostname,
		"process": map[string]interface{}{
			"terminal": cfg.TTY,
			"user":     map[string]int{"uid": 0, "gid": 0},
			"args":     args,
			"env":      env,
			"cwd":      cwd,
			"noNewPrivileges": false,
		},
		"root": map[string]interface{}{
			"path":     filepath.Join(MyDockerRoot, "containers", containerID, "merged"),
			"readonly": false,
		},
		"linux": map[string]interface{}{
			"namespaces": []map[string]string{
				{"type": "pid"}, {"type": "network"},
				{"type": "ipc"}, {"type": "uts"}, {"type": "mount"},
			},
			"cgroupsPath": "/mydocker/" + containerID,
			"rootfsPropagation": "rprivate",
		},
	}

	data, _ := json.MarshalIndent(spec, "", "  ")
	return data
}
