package oci

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// Runtime wraps an OCI-compatible runtime (runc, crun, etc.)
type Runtime struct {
	binary string // path to runtime binary e.g. /usr/bin/runc
}

// NewRuntime returns a Runtime using the given binary
// Falls back to mydocker's native runtime if binary not found
func NewRuntime(binary string) *Runtime {
	if binary == "" {
		binary = findRuntime()
	}
	return &Runtime{binary: binary}
}

// findRuntime finds an available OCI runtime
func findRuntime() string {
	candidates := []string{"runc", "crun", "kata-runtime"}
	for _, r := range candidates {
		if path, err := exec.LookPath(r); err == nil {
			return path
		}
	}
	return "" // use native
}

// IsAvailable returns true if an external OCI runtime is available
func (r *Runtime) IsAvailable() bool {
	return r.binary != ""
}

// RuntimeName returns the name of the runtime
func (r *Runtime) RuntimeName() string {
	if r.binary == "" {
		return "mydocker-native"
	}
	return filepath.Base(r.binary)
}

// CreateBundle creates an OCI bundle (rootfs + config.json) for a container
func CreateBundle(bundleDir string, spec *Spec, rootfsPath string) error {
	if err := os.MkdirAll(bundleDir, 0755); err != nil {
		return fmt.Errorf("mkdir bundle: %w", err)
	}

	// Create rootfs symlink or copy
	rootfsDst := filepath.Join(bundleDir, "rootfs")
	if err := os.Symlink(rootfsPath, rootfsDst); err != nil && !os.IsExist(err) {
		// If symlink fails, try bind mount
		if err := os.MkdirAll(rootfsDst, 0755); err != nil {
			return err
		}
		if err := syscall.Mount(rootfsPath, rootfsDst, "", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
			return fmt.Errorf("bind rootfs: %w", err)
		}
	}

	// Update spec root path to bundle-relative "rootfs"
	spec.Root.Path = "rootfs"

	return SaveSpec(spec, bundleDir)
}

// Run runs a container using the external OCI runtime
func (r *Runtime) Run(containerID, bundleDir string) error {
	if !r.IsAvailable() {
		return fmt.Errorf("no OCI runtime available")
	}

	cmd := exec.Command(r.binary, "run", "--bundle", bundleDir, containerID)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// Start starts a created container
func (r *Runtime) Start(containerID string) error {
	cmd := exec.Command(r.binary, "start", containerID)
	return cmd.Run()
}

// Kill sends a signal to a container
func (r *Runtime) Kill(containerID, signal string) error {
	cmd := exec.Command(r.binary, "kill", containerID, signal)
	return cmd.Run()
}

// Delete deletes a container
func (r *Runtime) Delete(containerID string) error {
	cmd := exec.Command(r.binary, "delete", containerID)
	return cmd.Run()
}

// State returns container state from the runtime
func (r *Runtime) State(containerID string) (*RuntimeState, error) {
	out, err := exec.Command(r.binary, "state", containerID).Output()
	if err != nil {
		return nil, err
	}
	var state RuntimeState
	return &state, json.Unmarshal(out, &state)
}

// RuntimeState is the OCI container state
type RuntimeState struct {
	OciVersion  string            `json:"ociVersion"`
	ID          string            `json:"id"`
	Status      string            `json:"status"`
	PID         int               `json:"pid,omitempty"`
	Bundle      string            `json:"bundle"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// ExportBundle exports a container as an OCI bundle tar
func ExportBundle(containerID, bundleDir, outputPath string) error {
	cmd := exec.Command("tar", "-czf", outputPath, "-C", filepath.Dir(bundleDir), filepath.Base(bundleDir))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ImportBundle imports an OCI bundle tar
func ImportBundle(tarPath, destDir string) error {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}
	cmd := exec.Command("tar", "-xzf", tarPath, "-C", destDir)
	return cmd.Run()
}
