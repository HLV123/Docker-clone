package container

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// IsRootless returns true if running as non-root
func IsRootless() bool {
	return os.Getuid() != 0
}

// SetupUserNamespace configures UID/GID mapping for rootless containers
// Maps host UID -> container root (UID 0)
func SetupUserNamespace(pid int) error {
	uid := os.Getuid()
	gid := os.Getgid()

	// Try newuidmap/newgidmap first (requires /etc/subuid)
	if err := tryNewuidmap(pid, uid); err != nil {
		// Fallback: direct write to /proc/<pid>/uid_map
		if err2 := writeIDMap(pid, uid, "uid_map"); err2 != nil {
			return fmt.Errorf("uid map: %w", err2)
		}
		// Must write "deny" to setgroups before gid_map
		_ = os.WriteFile(fmt.Sprintf("/proc/%d/setgroups", pid), []byte("deny"), 0)
		if err2 := writeIDMap(pid, gid, "gid_map"); err2 != nil {
			return fmt.Errorf("gid map: %w", err2)
		}
	}

	return nil
}

func tryNewuidmap(pid, uid int) error {
	// Check if newuidmap exists
	newuidmap, err := exec.LookPath("newuidmap")
	if err != nil {
		return err
	}
	newgidmap, err := exec.LookPath("newgidmap")
	if err != nil {
		return err
	}

	// newuidmap <pid> 0 <hostUID> 1 1 100000 65536
	pidStr := strconv.Itoa(pid)
	uidStr := strconv.Itoa(uid)

	cmd := exec.Command(newuidmap, pidStr, "0", uidStr, "1", "1", "100000", "65536")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("newuidmap: %w (%s)", err, out)
	}

	gid := os.Getgid()
	gidStr := strconv.Itoa(gid)
	cmd = exec.Command(newgidmap, pidStr, "0", gidStr, "1", "1", "100000", "65536")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("newgidmap: %w (%s)", err, out)
	}

	return nil
}

func writeIDMap(pid, hostID int, mapFile string) error {
	// Format: containerID hostID count
	// Map container root (0) to our UID, plus 65536 more
	mapping := fmt.Sprintf("0 %d 1\n1 100000 65536\n", hostID)
	path := fmt.Sprintf("/proc/%d/%s", pid, mapFile)
	return os.WriteFile(path, []byte(mapping), 0)
}

// RootlessCloneFlags adds user namespace flag for rootless operation
func RootlessCloneFlags(flags uintptr) uintptr {
	return flags | uintptr(syscall.CLONE_NEWUSER)
}

// SetupRootlessDirs creates necessary dirs with proper permissions for rootless
func SetupRootlessDirs() error {
	dirs := []string{
		"/var/lib/mydocker",
		"/var/lib/mydocker/images",
		"/var/lib/mydocker/containers",
		"/var/lib/mydocker/dns",
		"/sys/fs/cgroup/mydocker",
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			// Try with sudo if permission denied
			cmd := exec.Command("sudo", "mkdir", "-p", dir)
			if out, err2 := cmd.CombinedOutput(); err2 != nil {
				fmt.Fprintf(os.Stderr, "warning: mkdir %s: %s\n", dir, out)
			}
			cmd = exec.Command("sudo", "chown", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), dir)
			_ = cmd.Run()
		}
	}
	return nil
}
