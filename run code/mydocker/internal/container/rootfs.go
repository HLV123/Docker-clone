package container

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// SetupRootfs mount overlayfs và pivot_root vào đó
func SetupRootfs(containerID, imageName string) error {
	// Tìm image rootfs — thử nhiều format tên
	imageBase := findImageBase(imageName)
	if imageBase == "" {
		return fmt.Errorf("image not found: %s (run 'mydocker pull %s' first)", imageName, imageName)
	}

	containerDir := filepath.Join(MyDockerRoot, "containers", containerID)
	upperDir := filepath.Join(containerDir, "upper")
	workDir := filepath.Join(containerDir, "work")
	mergedDir := filepath.Join(containerDir, "merged")

	for _, dir := range []string{upperDir, workDir, mergedDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}

	// Build lowerdir — check for built image with base layers
	var lowerDir string
	dirName := imageDir(imageName)
	baseLayersFile := filepath.Join(MyDockerRoot, "images", dirName, "base_layers")
	if _, err := os.Stat(baseLayersFile); err == nil {
		lowerDir = buildLowerDirWithBase(imageName)
	} else {
		lowerDir = buildLowerDir(imageBase)
	}

	// Mount overlayfs
	opts := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s", lowerDir, upperDir, workDir)
	fmt.Fprintf(os.Stderr, "[DEBUG] lowerdir=%s\n", lowerDir)
	if err := syscall.Mount("overlay", mergedDir, "overlay", 0, opts); err != nil {
		return fmt.Errorf("mount overlay: %w", err)
	}

	// Copy resolv.conf từ host để container có DNS
	copyResolvConf(mergedDir)

	// pivot_root vào mergedDir
	if err := pivotRoot(mergedDir); err != nil {
		return fmt.Errorf("pivot_root: %w", err)
	}

	// Mount filesystems cơ bản sau pivot_root
	mounts := []struct{ src, dst, fstype string }{
		{"proc", "/proc", "proc"},
		{"sysfs", "/sys", "sysfs"},
		{"tmpfs", "/dev", "tmpfs"},
		{"tmpfs", "/tmp", "tmpfs"},
		{"tmpfs", "/run", "tmpfs"},
	}
	for _, m := range mounts {
		os.MkdirAll(m.dst, 0755)
		if err := syscall.Mount(m.src, m.dst, m.fstype, 0, ""); err != nil {
			fmt.Fprintf(os.Stderr, "mount %s: %v\n", m.dst, err)
		}
	}

	// Tạo device nodes tối thiểu
	setupDevices()

	return nil
}

// findImageBase tìm thư mục rootfs của image
// imageDir converts "alpine:3.18" -> "alpine_3.18" to avoid colon in path
// (overlayfs lowerdir uses : as separator)
func imageDir(imageName string) string {
	name := imageName
	if !strings.Contains(name, ":") {
		name = name + ":latest"
	}
	return strings.ReplaceAll(name, ":", "_")
}

func findImageBase(imageName string) string {
	dirName := imageDir(imageName)

	// Check layers directory (multi-layer image from pull)
	layersDir := filepath.Join(MyDockerRoot, "images", dirName, "layers")
	if entries, err := os.ReadDir(layersDir); err == nil && len(entries) > 0 {
		return layersDir
	}

	// Legacy: single base layer
	legacyCandidates := []string{
		filepath.Join(MyDockerRoot, "images", dirName, "rootfs"),
		filepath.Join(MyDockerRoot, "images", dirName, "layers", "base"),
	}
	for _, c := range legacyCandidates {
		if _, err := os.Stat(filepath.Join(c, "bin")); err == nil {
			return c
		}
	}
	return ""
}

// buildLowerDirWithBase builds lowerdir including base image layers for built images
func buildLowerDirWithBase(imageName string) string {
	dirName := imageDir(imageName)
	imageDir2 := filepath.Join(MyDockerRoot, "images", dirName)
	layersDir := filepath.Join(imageDir2, "layers")

	// Check for base_layers reference (built images)
	baseLayersFile := filepath.Join(imageDir2, "base_layers")
	baseLayersData, baseErr := os.ReadFile(baseLayersFile)

	// Get our build layers
	buildLayers := []string{}
	if entries, err := os.ReadDir(layersDir); err == nil {
		for i := len(entries) - 1; i >= 0; i-- {
			buildLayers = append(buildLayers, filepath.Join(layersDir, entries[i].Name()))
		}
	}

	if baseErr == nil && len(baseLayersData) > 0 {
		// Add base image layers after build layers
		baseLayersDir := strings.TrimSpace(string(baseLayersData))
		baseLayers := []string{}
		if entries, err := os.ReadDir(baseLayersDir); err == nil {
			for i := len(entries) - 1; i >= 0; i-- {
				baseLayers = append(baseLayers, filepath.Join(baseLayersDir, entries[i].Name()))
			}
		}
		allLayers := append(buildLayers, baseLayers...)
		if len(allLayers) > 0 {
			return strings.Join(allLayers, ":")
		}
	}

	if len(buildLayers) > 0 {
		return strings.Join(buildLayers, ":")
	}
	return layersDir
}

// buildLowerDir tạo chuỗi lowerdir cho overlayfs
// imageBase là path đến layers/ directory hoặc single base dir
func buildLowerDir(imageBase string) string {
	entries, err := os.ReadDir(imageBase)
	if err != nil || len(entries) == 0 {
		return imageBase
	}

	// Detect nếu đây là layers/ dir: entries là hex dirs (12 chars)
	isLayersDir := true
	for _, e := range entries {
		if !e.IsDir() || len(e.Name()) != 12 {
			isLayersDir = false
			break
		}
	}

	if isLayersDir {
		// Chain layers: last = top layer (highest priority) goes first
		dirs := make([]string, len(entries))
		for i, e := range entries {
			dirs[len(entries)-1-i] = filepath.Join(imageBase, e.Name())
		}
		return strings.Join(dirs, ":")
	}

	return imageBase
}

func pivotRoot(newRoot string) error {
	// Bind mount newRoot vào chính nó (pivot_root yêu cầu)
	if err := syscall.Mount(newRoot, newRoot, "", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
		return fmt.Errorf("bind mount: %w", err)
	}

	oldRoot := filepath.Join(newRoot, ".old_root")
	if err := os.MkdirAll(oldRoot, 0700); err != nil {
		return err
	}

	if err := syscall.PivotRoot(newRoot, oldRoot); err != nil {
		return fmt.Errorf("pivot_root syscall: %w", err)
	}

	if err := os.Chdir("/"); err != nil {
		return err
	}

	// Unmount old root và xóa
	if err := syscall.Unmount("/.old_root", syscall.MNT_DETACH); err != nil {
		return fmt.Errorf("unmount old root: %w", err)
	}

	return os.Remove("/.old_root")
}

func setupDevices() {
	type dev struct {
		path  string
		mode  uint32
		major int
		minor int
	}
	devices := []dev{
		{"/dev/null", syscall.S_IFCHR | 0666, 1, 3},
		{"/dev/zero", syscall.S_IFCHR | 0666, 1, 5},
		{"/dev/random", syscall.S_IFCHR | 0444, 1, 8},
		{"/dev/urandom", syscall.S_IFCHR | 0444, 1, 9},
		{"/dev/tty", syscall.S_IFCHR | 0666, 5, 0},
	}
	for _, d := range devices {
		devNum := mkdev(d.major, d.minor)
		_ = syscall.Mknod(d.path, d.mode, devNum)
	}
}

func mkdev(major, minor int) int {
	return (major << 8) | minor
}

func copyResolvConf(mergedDir string) {
	src := "/etc/resolv.conf"
	dst := filepath.Join(mergedDir, "etc", "resolv.conf")
	data, err := os.ReadFile(src)
	if err != nil {
		return
	}
	_ = os.WriteFile(dst, data, 0644)
}
