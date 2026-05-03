package oci

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GenerateSpec creates an OCI config.json for a container
func GenerateSpec(opts *SpecOptions) (*Spec, error) {
	spec := &Spec{
		Version:  OCIVersion,
		Hostname: opts.Hostname,
	}

	// Process
	args := opts.Command
	if len(args) == 0 {
		args = []string{"/bin/sh"}
	}

	spec.Process = &Process{
		Terminal: opts.TTY,
		User:     User{UID: 0, GID: 0},
		Args:     args,
		Env:      buildEnv(opts.Env),
		Cwd:      opts.WorkingDir,
		Capabilities: defaultCapabilities(),
		NoNewPrivileges: false,
		Rlimits: []POSIXRlimit{
			{Type: "RLIMIT_NOFILE", Hard: 1024, Soft: 1024},
		},
	}
	if spec.Process.Cwd == "" {
		spec.Process.Cwd = "/"
	}

	// Root filesystem
	spec.Root = &Root{
		Path:     opts.RootfsPath,
		Readonly: false,
	}

	// Mounts
	spec.Mounts = defaultMounts()
	for _, v := range opts.Volumes {
		mountOpts := []string{"bind", "rprivate"}
		if v.ReadOnly {
			mountOpts = append(mountOpts, "ro")
		}
		spec.Mounts = append(spec.Mounts, Mount{
			Destination: v.ContainerPath,
			Type:        "bind",
			Source:      v.HostPath,
			Options:     mountOpts,
		})
	}

	// Linux namespace config
	spec.Linux = &Linux{
		Namespaces: []LinuxNamespace{
			{Type: "pid"},
			{Type: "network"},
			{Type: "ipc"},
			{Type: "uts"},
			{Type: "mount"},
		},
		RootfsPropagation: "rprivate",
		MaskedPaths: []string{
			"/proc/acpi", "/proc/asound", "/proc/kcore",
			"/proc/keys", "/proc/latency_stats", "/proc/timer_list",
			"/proc/timer_stats", "/proc/sched_debug", "/proc/scsi",
			"/sys/firmware",
		},
		ReadonlyPaths: []string{
			"/proc/bus", "/proc/fs", "/proc/irq",
			"/proc/sys", "/proc/sysrq-trigger",
		},
		CgroupsPath: fmt.Sprintf("/mydocker/%s", opts.ContainerID),
	}

	// Resources
	if opts.MemoryLimit > 0 || opts.CPULimit > 0 || opts.PidsLimit > 0 {
		spec.Linux.Resources = &LinuxResources{}
		if opts.MemoryLimit > 0 {
			limit := opts.MemoryLimit
			spec.Linux.Resources.Memory = &LinuxMemory{Limit: &limit}
		}
		if opts.CPULimit > 0 {
			period := uint64(100000)
			quota := int64(opts.CPULimit * float64(period))
			spec.Linux.Resources.CPU = &LinuxCPU{
				Period: &period,
				Quota:  &quota,
			}
		}
		if opts.PidsLimit > 0 {
			spec.Linux.Resources.Pids = &LinuxPids{Limit: int64(opts.PidsLimit)}
		}
	}

	// Devices
	spec.Linux.Devices = defaultDevices()

	return spec, nil
}

// SaveSpec writes config.json to the container bundle directory
func SaveSpec(spec *Spec, bundleDir string) error {
	if err := os.MkdirAll(bundleDir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(bundleDir, "config.json"), data, 0644)
}

// LoadSpec reads config.json from a bundle directory
func LoadSpec(bundleDir string) (*Spec, error) {
	data, err := os.ReadFile(filepath.Join(bundleDir, "config.json"))
	if err != nil {
		return nil, err
	}
	var spec Spec
	return &spec, json.Unmarshal(data, &spec)
}

// SpecOptions holds inputs for spec generation
type SpecOptions struct {
	ContainerID string
	Image       string
	Command     []string
	Env         []string
	WorkingDir  string
	Hostname    string
	RootfsPath  string
	TTY         bool
	MemoryLimit int64
	CPULimit    float64
	PidsLimit   int
	Volumes     []VolumeOpt
}

type VolumeOpt struct {
	HostPath      string
	ContainerPath string
	ReadOnly      bool
}

func buildEnv(env []string) []string {
	base := []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/root",
		"TERM=xterm",
	}
	return append(base, env...)
}

func defaultMounts() []Mount {
	return []Mount{
		{
			Destination: "/proc",
			Type:        "proc",
			Source:      "proc",
			Options:     []string{"nosuid", "noexec", "nodev"},
		},
		{
			Destination: "/dev",
			Type:        "tmpfs",
			Source:      "tmpfs",
			Options:     []string{"nosuid", "strictatime", "mode=755", "size=65536k"},
		},
		{
			Destination: "/dev/pts",
			Type:        "devpts",
			Source:      "devpts",
			Options:     []string{"nosuid", "noexec", "newinstance", "ptmxmode=0666", "mode=0620"},
		},
		{
			Destination: "/dev/shm",
			Type:        "tmpfs",
			Source:      "shm",
			Options:     []string{"nosuid", "noexec", "nodev", "mode=1777", "size=65536k"},
		},
		{
			Destination: "/sys",
			Type:        "sysfs",
			Source:      "sysfs",
			Options:     []string{"nosuid", "noexec", "nodev", "ro"},
		},
		{
			Destination: "/tmp",
			Type:        "tmpfs",
			Source:      "tmpfs",
			Options:     []string{"nosuid", "nodev"},
		},
	}
}

func defaultCapabilities() *LinuxCapabilities {
	caps := []string{
		"CAP_CHOWN", "CAP_DAC_OVERRIDE", "CAP_FSETID", "CAP_FOWNER",
		"CAP_MKNOD", "CAP_NET_RAW", "CAP_SETGID", "CAP_SETUID",
		"CAP_SETFCAP", "CAP_SETPCAP", "CAP_NET_BIND_SERVICE",
		"CAP_SYS_CHROOT", "CAP_KILL", "CAP_AUDIT_WRITE",
	}
	return &LinuxCapabilities{
		Bounding:    caps,
		Effective:   caps,
		Permitted:   caps,
		Inheritable: []string{},
		Ambient:     []string{},
	}
}

func defaultDevices() []LinuxDevice {
	fm0666 := uint32(0666)
	uid0 := uint32(0)
	gid0 := uint32(0)
	gid5 := uint32(5)

	return []LinuxDevice{
		{Path: "/dev/null", Type: "c", Major: 1, Minor: 3, FileMode: &fm0666, UID: &uid0, GID: &gid0},
		{Path: "/dev/zero", Type: "c", Major: 1, Minor: 5, FileMode: &fm0666, UID: &uid0, GID: &gid0},
		{Path: "/dev/full", Type: "c", Major: 1, Minor: 7, FileMode: &fm0666, UID: &uid0, GID: &gid0},
		{Path: "/dev/random", Type: "c", Major: 1, Minor: 8, FileMode: &fm0666, UID: &uid0, GID: &gid0},
		{Path: "/dev/urandom", Type: "c", Major: 1, Minor: 9, FileMode: &fm0666, UID: &uid0, GID: &gid0},
		{Path: "/dev/tty", Type: "c", Major: 5, Minor: 0, FileMode: &fm0666, UID: &uid0, GID: &uid0},
		{Path: "/dev/ptmx", Type: "c", Major: 5, Minor: 2, FileMode: &fm0666, UID: &uid0, GID: &gid5},
	}
}

// ValidateSpec checks that a spec is valid
func ValidateSpec(spec *Spec) []string {
	errors := []string{}

	if spec.Version == "" {
		errors = append(errors, "ociVersion is required")
	}
	if spec.Process == nil {
		errors = append(errors, "process is required")
	} else {
		if len(spec.Process.Args) == 0 {
			errors = append(errors, "process.args is required")
		}
		if spec.Process.Cwd == "" {
			errors = append(errors, "process.cwd is required")
		} else if !strings.HasPrefix(spec.Process.Cwd, "/") {
			errors = append(errors, "process.cwd must be absolute")
		}
	}
	if spec.Root == nil {
		errors = append(errors, "root is required")
	} else if spec.Root.Path == "" {
		errors = append(errors, "root.path is required")
	}

	return errors
}
