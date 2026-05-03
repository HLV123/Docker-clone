package compose

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"mydocker/internal/build"
	"mydocker/internal/container"
	"mydocker/internal/image"
	"mydocker/internal/state"
)

const projectLabel = "compose.project"

// Runner manages compose lifecycle
type Runner struct {
	projectName string
	projectDir  string
	cf          *ComposeFile
	services    []*ResolvedService
}

func NewRunner(composePath string) (*Runner, error) {
	absPath, err := filepath.Abs(composePath)
	if err != nil {
		return nil, err
	}
	projectDir := filepath.Dir(absPath)
	projectName := filepath.Base(projectDir)

	cf, err := Parse(absPath)
	if err != nil {
		return nil, err
	}

	services, err := Resolve(cf, projectDir)
	if err != nil {
		return nil, err
	}

	return &Runner{
		projectName: projectName,
		projectDir:  projectDir,
		cf:          cf,
		services:    services,
	}, nil
}

// Up starts all services
func (r *Runner) Up(detach bool) error {
	fmt.Printf("[%s] Starting services...\n", r.projectName)

	for _, svc := range r.services {
		if err := r.startService(svc, detach); err != nil {
			return fmt.Errorf("service %s: %w", svc.Name, err)
		}
	}

	if !detach {
		fmt.Printf("\n[%s] All services started. Press Ctrl+C to stop.\n", r.projectName)
		// Wait for interrupt
		sig := make(chan os.Signal, 1)
		<-sig
	}

	return nil
}

// Down stops and removes all services
func (r *Runner) Down() error {
	fmt.Printf("[%s] Stopping services...\n", r.projectName)

	// Stop in reverse order
	for i := len(r.services) - 1; i >= 0; i-- {
		svc := r.services[i]
		r.stopService(svc)
	}

	fmt.Printf("[%s] All services stopped.\n", r.projectName)
	return nil
}

// PS lists service status
func (r *Runner) PS() error {
	fmt.Printf("%-20s %-15s %-10s %-20s\n", "NAME", "IMAGE", "STATUS", "PORTS")
	fmt.Println(fmt.Sprintf("%s", "─────────────────────────────────────────────────────────"))

	for _, svc := range r.services {
		containerName := r.projectName + "_" + svc.Name
		s := r.findContainer(containerName)
		status := "not created"
		ports := "-"
		image := svc.Image

		if s != nil {
			status = s.Status
			if len(s.Ports) > 0 {
				p := s.Ports[0]
				ports = fmt.Sprintf("%d->%d", p.HostPort, p.ContainerPort)
			}
		}

		fmt.Printf("%-20s %-15s %-10s %-20s\n",
			containerName,
			truncate(image, 15),
			status,
			ports,
		)
	}
	return nil
}

// Logs shows logs for all or specific services
func (r *Runner) Logs(services []string) error {
	targets := r.services
	if len(services) > 0 {
		targets = []*ResolvedService{}
		for _, name := range services {
			for _, svc := range r.services {
				if svc.Name == name {
					targets = append(targets, svc)
				}
			}
		}
	}

	for _, svc := range targets {
		containerName := r.projectName + "_" + svc.Name
		s := r.findContainer(containerName)
		if s == nil {
			continue
		}
		logPath := filepath.Join("/var/lib/mydocker/containers", s.ID, "container.log")
		data, err := os.ReadFile(logPath)
		if err != nil {
			continue
		}
		fmt.Printf("=== %s ===\n%s\n", svc.Name, string(data))
	}
	return nil
}

func (r *Runner) startService(svc *ResolvedService, detach bool) error {
	containerName := r.projectName + "_" + svc.Name

	// Check if already running
	existing := r.findContainer(containerName)
	if existing != nil && existing.Status == "running" {
		fmt.Printf("  [%s] Already running\n", svc.Name)
		return nil
	}

	// Build image if needed
	if svc.ShouldBuild {
		fmt.Printf("  [%s] Building image %s...\n", svc.Name, svc.Image)
		buildCfg := &build.BuildConfig{
			Tag:        svc.Image,
			ContextDir: svc.BuildCtx,
			Dockerfile: svc.BuildFile,
		}
		if err := build.Build(buildCfg); err != nil {
			return fmt.Errorf("build: %w", err)
		}
	} else {
		// Pull image if not available
		if !imageExists(svc.Image) {
			fmt.Printf("  [%s] Pulling %s...\n", svc.Name, svc.Image)
			if err := image.Pull(svc.Image); err != nil {
				return fmt.Errorf("pull %s: %w", svc.Image, err)
			}
		}
	}

	fmt.Printf("  [%s] Starting container...\n", svc.Name)

	cfg := &container.Config{
		Image:       svc.Image,
		Command:     svc.Command,
		Entrypoint:  svc.Entrypoint,
		Env:         svc.Env,
		Hostname:    svc.Hostname,
		Name:        containerName,
		WorkingDir:  svc.WorkingDir,
		MemoryLimit: svc.MemLimit,
		Network:     true,
		Detach:      true, // always detach in compose
	}

	for _, p := range svc.Ports {
		cfg.Ports = append(cfg.Ports, container.PortMapping{
			HostPort:      p.HostPort,
			ContainerPort: p.ContainerPort,
			Protocol:      p.Protocol,
		})
	}

	for _, v := range svc.Volumes {
		cfg.Volumes = append(cfg.Volumes, container.VolumeMount{
			HostPath:      v.HostPath,
			ContainerPath: v.ContainerPath,
			ReadOnly:      v.ReadOnly,
		})
	}

	container.Run(cfg)

	// Small delay to let container start
	time.Sleep(500 * time.Millisecond)
	fmt.Printf("  [%s] Started ✓\n", svc.Name)
	return nil
}

func (r *Runner) stopService(svc *ResolvedService) {
	containerName := r.projectName + "_" + svc.Name
	s := r.findContainer(containerName)
	if s == nil || s.Status != "running" {
		return
	}
	fmt.Printf("  [%s] Stopping...\n", svc.Name)
	_ = syscall.Kill(s.PID, syscall.SIGTERM)

	// Wait up to 10s
	for i := 0; i < 100; i++ {
		time.Sleep(100 * time.Millisecond)
		if syscall.Kill(s.PID, 0) != nil {
			break
		}
	}
	_ = syscall.Kill(s.PID, syscall.SIGKILL)

	// Cleanup
	mergedDir := filepath.Join("/var/lib/mydocker/containers", s.ID, "merged")
	_ = syscall.Unmount(mergedDir, syscall.MNT_DETACH)

	s.Status = "exited"
	_ = state.Save(s)
	fmt.Printf("  [%s] Stopped ✓\n", svc.Name)
}

// findContainer finds a container by name
func (r *Runner) findContainer(name string) *state.State {
	entries, err := os.ReadDir("/var/lib/mydocker/containers")
	if err != nil {
		return nil
	}
	for _, e := range entries {
		s, err := state.Load(e.Name())
		if err != nil {
			continue
		}
		if s.Name == name {
			// Sync status
			if s.Status == "running" && s.PID > 0 {
				if syscall.Kill(s.PID, 0) != nil {
					s.Status = "exited"
					_ = state.Save(s)
				}
			}
			return s
		}
	}
	return nil
}

// imageExists checks if an image is available locally
func imageExists(imageName string) bool {
	name, tag := parseImageRef(imageName)
	dirName := name + "_" + tag
	layersDir := filepath.Join("/var/lib/mydocker/images", dirName, "layers")
	entries, err := os.ReadDir(layersDir)
	return err == nil && len(entries) > 0
}

func parseImageRef(ref string) (name, tag string) {
	parts := splitN(ref, ":", 2)
	name = parts[0]
	tag = "latest"
	if len(parts) == 2 {
		tag = parts[1]
	}
	return
}

func splitN(s, sep string, n int) []string {
	idx := len(s)
	if i := lastIndex(s, sep); i != -1 {
		idx = i
	}
	if n == 2 && idx < len(s) {
		return []string{s[:idx], s[idx+1:]}
	}
	return []string{s}
}

func lastIndex(s, sub string) int {
	for i := len(s) - len(sub); i >= 0; i-- {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

// ExecService runs a command in a running service container
func (r *Runner) ExecService(serviceName string, cmd []string) error {
	containerName := r.projectName + "_" + serviceName
	s := r.findContainer(containerName)
	if s == nil {
		return fmt.Errorf("service %s not found", serviceName)
	}
	if s.Status != "running" {
		return fmt.Errorf("service %s is not running", serviceName)
	}

	nsenterArgs := []string{fmt.Sprintf("--target=%d", s.PID), "--mount", "--uts", "--ipc", "--pid", "--"}
	nsenterArgs = append(nsenterArgs, cmd...)
	c := exec.Command("nsenter", nsenterArgs...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
