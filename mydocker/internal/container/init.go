package container

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"mydocker/internal/network"
	"mydocker/internal/state"
)

func Init() {
	if len(os.Args) < 6 {
		fmt.Fprintln(os.Stderr, "init: insufficient args")
		os.Exit(1)
	}
	containerID := os.Args[2]
	imageName := os.Args[3]
	command := os.Args[5]
	args := os.Args[5:]

	cfg, _ := state.LoadConfig(containerID)

	if err := syscall.Mount("", "/", "", syscall.MS_PRIVATE|syscall.MS_REC, ""); err != nil {
		fmt.Fprintf(os.Stderr, "make-private: %v\n", err)
		os.Exit(1)
	}

	containerDir := filepath.Join(MyDockerRoot, "containers", containerID)
	upperDir := filepath.Join(containerDir, "upper")
	workDir := filepath.Join(containerDir, "work")
	mergedDir := filepath.Join(containerDir, "merged")

	for _, d := range []string{upperDir, workDir, mergedDir} {
		_ = os.MkdirAll(d, 0755)
	}

	imageBase := findImageBase(imageName)
	if imageBase == "" {
		fmt.Fprintf(os.Stderr, "image not found: %s\n", imageName)
		os.Exit(1)
	}

	// Mount overlayfs
	lowerDir := buildLowerDir(imageBase)
	opts := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s", lowerDir, upperDir, workDir)
	if err := syscall.Mount("overlay", mergedDir, "overlay", 0, opts); err != nil {
		fmt.Fprintf(os.Stderr, "mount overlay: %v\n", err)
		os.Exit(1)
	}

	// Bind mount volumes INTO mergedDir BEFORE pivot_root
	for _, v := range cfg.Volumes {
		target := filepath.Join(mergedDir, v.ContainerPath)
		if err := os.MkdirAll(target, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "mkdir volume target %s: %v\n", target, err)
			continue
		}
		flags := uintptr(syscall.MS_BIND | syscall.MS_REC)
		if err := syscall.Mount(v.HostPath, target, "", flags, ""); err != nil {
			fmt.Fprintf(os.Stderr, "volume %s->%s: %v\n", v.HostPath, v.ContainerPath, err)
			continue
		}
		if v.ReadOnly {
			_ = syscall.Mount("", target, "", syscall.MS_BIND|syscall.MS_REMOUNT|syscall.MS_RDONLY, "")
		}
	}

	// Inject DNS hosts (container name resolution)
	network.InjectDNS(mergedDir)

	if err := pivotRoot(mergedDir); err != nil {
		fmt.Fprintf(os.Stderr, "pivot_root: %v\n", err)
		os.Exit(1)
	}

	// Mount /proc /sys /dev /tmp /run
	for _, m := range []struct{ src, dst, fs string }{
		{"proc", "/proc", "proc"},
		{"sysfs", "/sys", "sysfs"},
		{"tmpfs", "/dev", "tmpfs"},
		{"tmpfs", "/tmp", "tmpfs"},
		{"tmpfs", "/run", "tmpfs"},
	} {
		_ = os.MkdirAll(m.dst, 0755)
		_ = syscall.Mount(m.src, m.dst, m.fs, 0, "")
	}
	setupDevices()

	// Hostname
	hostname := cfg.Hostname
	if hostname == "" {
		hostname = "container-" + containerID[:6]
	}
	_ = syscall.Sethostname([]byte(hostname))

	// Working directory
	if cfg.WorkingDir != "" {
		if err := os.Chdir(cfg.WorkingDir); err != nil {
			_ = os.Chdir("/")
		}
	}

	path, err := exec.LookPath(command)
	if err != nil {
		fmt.Fprintf(os.Stderr, "command not found: %s\n", command)
		os.Exit(1)
	}

	if err := syscall.Exec(path, args, os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "exec: %v\n", err)
		os.Exit(1)
	}
}

func mountVolume(hostPath, containerPath string, readOnly bool) error {
	if err := os.MkdirAll(containerPath, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", containerPath, err)
	}
	flags := uintptr(syscall.MS_BIND | syscall.MS_REC)
	if err := syscall.Mount(hostPath, containerPath, "", flags, ""); err != nil {
		return fmt.Errorf("bind mount: %w", err)
	}
	if readOnly {
		if err := syscall.Mount("", containerPath, "", syscall.MS_BIND|syscall.MS_REMOUNT|syscall.MS_RDONLY, ""); err != nil {
			return fmt.Errorf("remount ro: %w", err)
		}
	}
	return nil
}
