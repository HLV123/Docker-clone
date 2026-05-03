package overlay

import (
	"fmt"
	"os"
	"syscall"
)

type OverlayMount struct {
	LowerDir  string
	UpperDir  string
	WorkDir   string
	MergedDir string
}

func Mount(o *OverlayMount) error {
	for _, dir := range []string{o.UpperDir, o.WorkDir, o.MergedDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	opts := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s",
		o.LowerDir, o.UpperDir, o.WorkDir)
	return syscall.Mount("overlay", o.MergedDir, "overlay", 0, opts)
}

func Unmount(mergedDir string) error {
	return syscall.Unmount(mergedDir, syscall.MNT_DETACH)
}
