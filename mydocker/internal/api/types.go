package api

// Request/Response types for the mydocker daemon API

// CreateContainerRequest is sent by CLI to create a container
type CreateContainerRequest struct {
	Image       string            `json:"image"`
	Command     []string          `json:"command"`
	Entrypoint  []string          `json:"entrypoint,omitempty"`
	Env         []string          `json:"env,omitempty"`
	Ports       []PortMapping     `json:"ports,omitempty"`
	Volumes     []VolumeMount     `json:"volumes,omitempty"`
	Name        string            `json:"name,omitempty"`
	Hostname    string            `json:"hostname,omitempty"`
	WorkingDir  string            `json:"working_dir,omitempty"`
	MemoryLimit int64             `json:"memory_limit,omitempty"`
	CPULimit    float64           `json:"cpu_limit,omitempty"`
	PidsLimit   int               `json:"pids_limit,omitempty"`
	Network     bool              `json:"network"`
	TTY         bool              `json:"tty"`
	Interactive bool              `json:"interactive"`
	Detach      bool              `json:"detach"`
	RestartPolicy string          `json:"restart_policy,omitempty"` // "", "always", "on-failure", "unless-stopped"
	Labels      map[string]string `json:"labels,omitempty"`
}

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

// CreateContainerResponse is returned after creating a container
type CreateContainerResponse struct {
	ID      string `json:"id"`
	Warning string `json:"warning,omitempty"`
}

// StartContainerRequest for starting a container (attach stdin/stdout)
type StartContainerRequest struct {
	AttachStdin  bool `json:"attach_stdin"`
	AttachStdout bool `json:"attach_stdout"`
	AttachStderr bool `json:"attach_stderr"`
}

// ContainerInfo is returned in list/inspect
type ContainerInfo struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Image     string       `json:"image"`
	Command   []string     `json:"command"`
	Status    string       `json:"status"`
	PID       int          `json:"pid"`
	IP        string       `json:"ip,omitempty"`
	Ports     []PortMapping `json:"ports,omitempty"`
	CreatedAt string       `json:"created_at"`
	ExitCode  *int         `json:"exit_code,omitempty"`
}

// PullRequest for pulling an image
type PullRequest struct {
	Image string `json:"image"`
}

// ImageInfo for listing images
type ImageInfo struct {
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	Layers     int    `json:"layers"`
	Size       int64  `json:"size"`
}

// ExecRequest for exec into container
type ExecRequest struct {
	Command []string `json:"command"`
	TTY     bool     `json:"tty"`
}

// BuildRequest for building an image
type BuildRequest struct {
	Tag        string `json:"tag"`
	Dockerfile string `json:"dockerfile"` // base64 encoded
	Context    string `json:"context"`    // base64 encoded tar
	NoCache    bool   `json:"no_cache"`
}

// ErrorResponse for API errors
type ErrorResponse struct {
	Error string `json:"error"`
}

// StatsResponse for container stats
type StatsResponse struct {
	ContainerID string `json:"container_id"`
	Status      string `json:"status"`
	PID         int    `json:"pid"`
	MemoryCurrent int64 `json:"memory_current"`
	MemoryMax     int64 `json:"memory_max"`
	CPUUsageUsec  int64 `json:"cpu_usage_usec"`
}
