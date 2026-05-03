package oci

// OCI Runtime Spec v1.0 - config.json structure
// https://github.com/opencontainers/runtime-spec/blob/main/config.md

// Spec is the top-level OCI runtime spec (config.json)
type Spec struct {
	Version     string      `json:"ociVersion"`
	Process     *Process    `json:"process,omitempty"`
	Root        *Root       `json:"root,omitempty"`
	Hostname    string      `json:"hostname,omitempty"`
	Mounts      []Mount     `json:"mounts,omitempty"`
	Hooks       *Hooks      `json:"hooks,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	Linux       *Linux      `json:"linux,omitempty"`
}

// Process contains the process spec
type Process struct {
	Terminal        bool            `json:"terminal,omitempty"`
	ConsoleSize     *ConsoleSize    `json:"consoleSize,omitempty"`
	User            User            `json:"user"`
	Args            []string        `json:"args"`
	Env             []string        `json:"env,omitempty"`
	Cwd             string          `json:"cwd"`
	Capabilities    *LinuxCapabilities `json:"capabilities,omitempty"`
	Rlimits         []POSIXRlimit   `json:"rlimits,omitempty"`
	NoNewPrivileges bool            `json:"noNewPrivileges,omitempty"`
}

type ConsoleSize struct {
	Height uint `json:"height"`
	Width  uint `json:"width"`
}

type User struct {
	UID            uint32   `json:"uid"`
	GID            uint32   `json:"gid"`
	AdditionalGids []uint32 `json:"additionalGids,omitempty"`
	Username       string   `json:"username,omitempty"`
}

type POSIXRlimit struct {
	Type string `json:"type"`
	Hard uint64 `json:"hard"`
	Soft uint64 `json:"soft"`
}

// Root contains the rootfs info
type Root struct {
	Path     string `json:"path"`
	Readonly bool   `json:"readonly,omitempty"`
}

// Mount represents a mount point
type Mount struct {
	Destination string   `json:"destination"`
	Type        string   `json:"type,omitempty"`
	Source      string   `json:"source,omitempty"`
	Options     []string `json:"options,omitempty"`
}

// Hooks for container lifecycle events
type Hooks struct {
	Prestart        []Hook `json:"prestart,omitempty"`
	CreateRuntime   []Hook `json:"createRuntime,omitempty"`
	CreateContainer []Hook `json:"createContainer,omitempty"`
	StartContainer  []Hook `json:"startContainer,omitempty"`
	Poststart       []Hook `json:"poststart,omitempty"`
	Poststop        []Hook `json:"poststop,omitempty"`
}

type Hook struct {
	Path    string   `json:"path"`
	Args    []string `json:"args,omitempty"`
	Env     []string `json:"env,omitempty"`
	Timeout *int     `json:"timeout,omitempty"`
}

// Linux-specific configuration
type Linux struct {
	UIDMappings     []LinuxIDMapping    `json:"uidMappings,omitempty"`
	GIDMappings     []LinuxIDMapping    `json:"gidMappings,omitempty"`
	Sysctl          map[string]string   `json:"sysctl,omitempty"`
	Resources       *LinuxResources     `json:"resources,omitempty"`
	CgroupsPath     string              `json:"cgroupsPath,omitempty"`
	Namespaces      []LinuxNamespace    `json:"namespaces,omitempty"`
	Devices         []LinuxDevice       `json:"devices,omitempty"`
	Seccomp         *LinuxSeccomp       `json:"seccomp,omitempty"`
	RootfsPropagation string            `json:"rootfsPropagation,omitempty"`
	MaskedPaths     []string            `json:"maskedPaths,omitempty"`
	ReadonlyPaths   []string            `json:"readonlyPaths,omitempty"`
}

type LinuxIDMapping struct {
	ContainerID uint32 `json:"containerID"`
	HostID      uint32 `json:"hostID"`
	Size        uint32 `json:"size"`
}

type LinuxNamespace struct {
	Type string `json:"type"`
	Path string `json:"path,omitempty"`
}

type LinuxResources struct {
	Memory  *LinuxMemory  `json:"memory,omitempty"`
	CPU     *LinuxCPU     `json:"cpu,omitempty"`
	Pids    *LinuxPids    `json:"pids,omitempty"`
}

type LinuxMemory struct {
	Limit       *int64 `json:"limit,omitempty"`
	Swap        *int64 `json:"swap,omitempty"`
	Reservation *int64 `json:"reservation,omitempty"`
}

type LinuxCPU struct {
	Shares *uint64 `json:"shares,omitempty"`
	Quota  *int64  `json:"quota,omitempty"`
	Period *uint64 `json:"period,omitempty"`
}

type LinuxPids struct {
	Limit int64 `json:"limit,omitempty"`
}

type LinuxDevice struct {
	Path     string `json:"path"`
	Type     string `json:"type"`
	Major    int64  `json:"major"`
	Minor    int64  `json:"minor"`
	FileMode *uint32 `json:"fileMode,omitempty"`
	UID      *uint32 `json:"uid,omitempty"`
	GID      *uint32 `json:"gid,omitempty"`
}

type LinuxCapabilities struct {
	Bounding    []string `json:"bounding,omitempty"`
	Effective   []string `json:"effective,omitempty"`
	Inheritable []string `json:"inheritable,omitempty"`
	Permitted   []string `json:"permitted,omitempty"`
	Ambient     []string `json:"ambient,omitempty"`
}

type LinuxSeccomp struct {
	DefaultAction string         `json:"defaultAction"`
	Architectures []string       `json:"architectures,omitempty"`
	Syscalls      []LinuxSyscall `json:"syscalls,omitempty"`
}

type LinuxSyscall struct {
	Names  []string `json:"names"`
	Action string   `json:"action"`
}

// OCI Version
const OCIVersion = "1.0.2"
