package compose

// ComposeFile represents a docker-compose.yml
type ComposeFile struct {
	Version  string             `yaml:"version"`
	Services map[string]Service `yaml:"services"`
	Volumes  map[string]Volume  `yaml:"volumes"`
	Networks map[string]Network `yaml:"networks"`
}

// Service represents a single service in compose
type Service struct {
	Image       string            `yaml:"image"`
	Build       interface{}       `yaml:"build"` // string or BuildConfig
	Command     interface{}       `yaml:"command"` // string or []string
	Entrypoint  interface{}       `yaml:"entrypoint"`
	Environment interface{}       `yaml:"environment"` // []string or map
	Ports       []string          `yaml:"ports"`
	Volumes     []string          `yaml:"volumes"`
	DependsOn   interface{}       `yaml:"depends_on"` // []string or map
	Restart     string            `yaml:"restart"`
	NetworkMode string            `yaml:"network_mode"`
	Networks    interface{}       `yaml:"networks"`
	Hostname    string            `yaml:"hostname"`
	Labels      map[string]string `yaml:"labels"`
	WorkingDir  string            `yaml:"working_dir"`
	User        string            `yaml:"user"`
	MemLimit    string            `yaml:"mem_limit"`
	CPUShares   int               `yaml:"cpu_shares"`
	HealthCheck *HealthCheck      `yaml:"healthcheck"`
	Name        string            // set from map key
}

type BuildConfig struct {
	Context    string `yaml:"context"`
	Dockerfile string `yaml:"dockerfile"`
	Args       map[string]string `yaml:"args"`
}

type HealthCheck struct {
	Test     []string `yaml:"test"`
	Interval string   `yaml:"interval"`
	Timeout  string   `yaml:"timeout"`
	Retries  int      `yaml:"retries"`
}

type Volume struct {
	Driver     string            `yaml:"driver"`
	DriverOpts map[string]string `yaml:"driver_opts"`
}

type Network struct {
	Driver string `yaml:"driver"`
}

// Resolved service with all fields parsed
type ResolvedService struct {
	Name        string
	Image       string
	Command     []string
	Entrypoint  []string
	Env         []string
	Ports       []PortMapping
	Volumes     []VolumeMount
	DependsOn   []string
	Restart     string
	Hostname    string
	WorkingDir  string
	MemLimit    int64
	BuildCtx    string
	BuildFile   string
	ShouldBuild bool
}

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
