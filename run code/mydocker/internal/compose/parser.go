package compose

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Parse reads and parses a docker-compose.yml file
func Parse(path string) (*ComposeFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var cf ComposeFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}

	// Set service names from map keys
	for name, svc := range cf.Services {
		svc.Name = name
		cf.Services[name] = svc
	}

	return &cf, nil
}

// Resolve converts a ComposeFile into ResolvedServices
func Resolve(cf *ComposeFile, projectDir string) ([]*ResolvedService, error) {
	services := []*ResolvedService{}

	for name, svc := range cf.Services {
		resolved, err := resolveService(name, svc, projectDir)
		if err != nil {
			return nil, fmt.Errorf("service %s: %w", name, err)
		}
		services = append(services, resolved)
	}

	// Sort by dependency order
	return topoSort(services), nil
}

func resolveService(name string, svc Service, projectDir string) (*ResolvedService, error) {
	r := &ResolvedService{
		Name:       name,
		Image:      svc.Image,
		Hostname:   svc.Hostname,
		WorkingDir: svc.WorkingDir,
		Restart:    svc.Restart,
	}

	if r.Hostname == "" {
		r.Hostname = name
	}

	// Command
	r.Command = parseStringOrList(svc.Command)
	r.Entrypoint = parseStringOrList(svc.Entrypoint)

	// Environment
	r.Env = parseEnvironment(svc.Environment)

	// Ports
	for _, p := range svc.Ports {
		pm, err := parsePort(p)
		if err != nil {
			return nil, fmt.Errorf("port %s: %w", p, err)
		}
		r.Ports = append(r.Ports, pm)
	}

	// Volumes
	for _, v := range svc.Volumes {
		vm, err := parseVolume(v, projectDir)
		if err != nil {
			return nil, fmt.Errorf("volume %s: %w", v, err)
		}
		r.Volumes = append(r.Volumes, vm)
	}

	// DependsOn
	r.DependsOn = parseDependsOn(svc.DependsOn)

	// Memory limit
	if svc.MemLimit != "" {
		r.MemLimit = parseMemory(svc.MemLimit)
	}

	// Build
	if svc.Build != nil {
		r.ShouldBuild = true
		switch b := svc.Build.(type) {
		case string:
			r.BuildCtx = filepath.Join(projectDir, b)
			r.BuildFile = filepath.Join(r.BuildCtx, "Dockerfile")
		case map[string]interface{}:
			if ctx, ok := b["context"].(string); ok {
				r.BuildCtx = filepath.Join(projectDir, ctx)
			} else {
				r.BuildCtx = projectDir
			}
			if df, ok := b["dockerfile"].(string); ok {
				r.BuildFile = filepath.Join(r.BuildCtx, df)
			} else {
				r.BuildFile = filepath.Join(r.BuildCtx, "Dockerfile")
			}
		}
		if r.Image == "" {
			r.Image = name + ":latest"
		}
	}

	return r, nil
}

// parseStringOrList handles both "cmd" and ["cmd", "arg"]
func parseStringOrList(v interface{}) []string {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case string:
		if val == "" {
			return nil
		}
		return []string{"/bin/sh", "-c", val}
	case []interface{}:
		result := []string{}
		for _, item := range val {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}

// parseEnvironment handles both list and map formats
func parseEnvironment(v interface{}) []string {
	if v == nil {
		return nil
	}
	result := []string{}
	switch env := v.(type) {
	case []interface{}:
		for _, item := range env {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
	case map[string]interface{}:
		for k, val := range env {
			if val == nil {
				// Get from host environment
				if hostVal := os.Getenv(k); hostVal != "" {
					result = append(result, k+"="+hostVal)
				}
			} else {
				result = append(result, fmt.Sprintf("%s=%v", k, val))
			}
		}
	}
	return result
}

// parsePort handles "8080:80", "8080:80/tcp", "80"
func parsePort(s string) (PortMapping, error) {
	protocol := "tcp"
	if idx := strings.LastIndex(s, "/"); idx != -1 {
		protocol = s[idx+1:]
		s = s[:idx]
	}

	parts := strings.SplitN(s, ":", 2)
	if len(parts) == 1 {
		// Just container port
		port, err := strconv.Atoi(parts[0])
		if err != nil {
			return PortMapping{}, err
		}
		return PortMapping{HostPort: port, ContainerPort: port, Protocol: protocol}, nil
	}

	hostPort, err := strconv.Atoi(parts[0])
	if err != nil {
		return PortMapping{}, fmt.Errorf("invalid host port: %s", parts[0])
	}
	containerPort, err := strconv.Atoi(parts[1])
	if err != nil {
		return PortMapping{}, fmt.Errorf("invalid container port: %s", parts[1])
	}
	return PortMapping{HostPort: hostPort, ContainerPort: containerPort, Protocol: protocol}, nil
}

// parseVolume handles "host:container", "host:container:ro", "named:/path"
func parseVolume(s, projectDir string) (VolumeMount, error) {
	parts := strings.SplitN(s, ":", 3)
	if len(parts) < 2 {
		return VolumeMount{}, fmt.Errorf("invalid volume: %s", s)
	}

	hostPath := parts[0]
	containerPath := parts[1]
	readOnly := len(parts) == 3 && parts[2] == "ro"

	// Resolve relative paths
	if !filepath.IsAbs(hostPath) && !strings.HasPrefix(hostPath, ".") {
		// Named volume — use /var/lib/mydocker/volumes/
		hostPath = filepath.Join("/var/lib/mydocker/volumes", hostPath)
	} else if !filepath.IsAbs(hostPath) {
		hostPath = filepath.Join(projectDir, hostPath)
	}
	_ = os.MkdirAll(hostPath, 0755)

	return VolumeMount{HostPath: hostPath, ContainerPath: containerPath, ReadOnly: readOnly}, nil
}

// parseDependsOn handles both []string and map formats
func parseDependsOn(v interface{}) []string {
	if v == nil {
		return nil
	}
	result := []string{}
	switch deps := v.(type) {
	case []interface{}:
		for _, d := range deps {
			if s, ok := d.(string); ok {
				result = append(result, s)
			}
		}
	case map[string]interface{}:
		for k := range deps {
			result = append(result, k)
		}
	}
	return result
}

// parseMemory converts "100m", "1g" to bytes
func parseMemory(s string) int64 {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) == 0 {
		return 0
	}
	unit := s[len(s)-1]
	numStr := s[:len(s)-1]
	num, err := strconv.ParseInt(numStr, 10, 64)
	if err != nil {
		return 0
	}
	switch unit {
	case 'k':
		return num * 1024
	case 'm':
		return num * 1024 * 1024
	case 'g':
		return num * 1024 * 1024 * 1024
	}
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

// topoSort sorts services by dependency order
func topoSort(services []*ResolvedService) []*ResolvedService {
	// Build index
	byName := map[string]*ResolvedService{}
	for _, s := range services {
		byName[s.Name] = s
	}

	visited := map[string]bool{}
	result := []*ResolvedService{}

	var visit func(name string)
	visit = func(name string) {
		if visited[name] {
			return
		}
		visited[name] = true
		svc, ok := byName[name]
		if !ok {
			return
		}
		for _, dep := range svc.DependsOn {
			visit(dep)
		}
		result = append(result, svc)
	}

	for _, s := range services {
		visit(s.Name)
	}
	return result
}
