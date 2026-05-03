package build

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"mydocker/internal/image"
)

const mydockerRoot = "/var/lib/mydocker"

// BuildConfig holds options for the build command
type BuildConfig struct {
	Tag        string // image:tag
	ContextDir string // build context directory
	Dockerfile string // path to Dockerfile
	NoCache    bool
}

// Build builds an image from a Dockerfile
func Build(cfg *BuildConfig) error {
	df, err := Parse(cfg.Dockerfile)
	if err != nil {
		return fmt.Errorf("parse Dockerfile: %w", err)
	}

	if len(df.Instructions) == 0 {
		return fmt.Errorf("empty Dockerfile")
	}

	// First instruction must be FROM
	if df.Instructions[0].Command != "FROM" {
		return fmt.Errorf("Dockerfile must start with FROM")
	}

	fmt.Printf("Building image %s...\n", cfg.Tag)
	fmt.Printf("Context: %s\n", cfg.ContextDir)
	fmt.Println()

	b := &builder{
		cfg:        cfg,
		df:         df,
		contextDir: cfg.ContextDir,
		buildEnv:   []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"},
		buildArgs:  map[string]string{},
	}

	return b.execute()
}

type builder struct {
	cfg        *BuildConfig
	df         *Dockerfile
	contextDir string
	currentDir string // working layer dir
	workDir    string // WORKDIR inside container
	buildEnv   []string
	buildArgs  map[string]string
	cmd        []string
	entrypoint []string
	layerDirs  []string // accumulated layers
}

func (b *builder) execute() error {
	for i, instr := range b.df.Instructions {
		fmt.Printf("Step %d/%d : %s\n", i+1, len(b.df.Instructions), instr.Raw)

		var err error
		switch instr.Command {
		case "FROM":
			err = b.handleFrom(instr)
		case "RUN":
			err = b.handleRun(instr)
		case "COPY", "ADD":
			err = b.handleCopy(instr)
		case "ENV":
			err = b.handleEnv(instr)
		case "WORKDIR":
			err = b.handleWorkdir(instr)
		case "CMD":
			b.cmd = instr.Args
			fmt.Printf(" ---> Setting CMD to %v\n", instr.Args)
		case "ENTRYPOINT":
			b.entrypoint = instr.Args
			fmt.Printf(" ---> Setting ENTRYPOINT to %v\n", instr.Args)
		case "EXPOSE":
			fmt.Printf(" ---> Exposing ports: %v\n", instr.Args)
		case "ARG":
			b.handleArg(instr)
		case "LABEL":
			fmt.Printf(" ---> Label: %s\n", strings.Join(instr.Args, " "))
		case "USER":
			fmt.Printf(" ---> USER: %s (noted, not enforced)\n", strings.Join(instr.Args, " "))
		default:
			fmt.Printf(" ---> Skipping unsupported instruction: %s\n", instr.Command)
		}

		if err != nil {
			return fmt.Errorf("step %d (%s): %w", i+1, instr.Command, err)
		}
	}

	// Save final image
	return b.saveImage()
}

func (b *builder) handleFrom(instr Instruction) error {
	baseImage := instr.Args[0]
	if baseImage == "scratch" {
		// Create empty base layer
		layerDir, err := b.newLayer("scratch")
		if err != nil {
			return err
		}
		b.currentDir = layerDir
		fmt.Printf(" ---> Using scratch (empty filesystem)\n")
		return nil
	}

	// Normalize image name
	name, tag := parseRef(baseImage)
	dirName := name + "_" + tag
	layersDir := filepath.Join(mydockerRoot, "images", dirName, "layers")

	if _, err := os.Stat(layersDir); os.IsNotExist(err) {
		// Auto-pull the base image
		fmt.Printf(" ---> Pulling base image %s...\n", baseImage)
		if err := image.Pull(baseImage); err != nil {
			return fmt.Errorf("pull %s: %w", baseImage, err)
		}
	}

	// Create a new writable layer on top of base
	layerDir, err := b.newLayer("from-" + tag)
	if err != nil {
		return err
	}

	// Copy base image layers list for reference
	b.currentDir = layerDir
	b.layerDirs = getBaseLayers(layersDir)
	fmt.Printf(" ---> Using base image %s (%d layers)\n", baseImage, len(b.layerDirs))
	return nil
}

func (b *builder) handleRun(instr Instruction) error {
	// Create new layer for this RUN
	layerDir, err := b.newLayer("run")
	if err != nil {
		return err
	}

	fmt.Printf(" ---> Running: %s\n", strings.Join(instr.Args, " "))

	// Mount overlayfs with all previous layers + new upper
	mergedDir := layerDir + "-merged"
	workDir := layerDir + "-work"
	_ = os.MkdirAll(mergedDir, 0755)
	_ = os.MkdirAll(workDir, 0755)

	lowerDir := strings.Join(b.layerDirs, ":")
	if lowerDir == "" {
		lowerDir = layerDir // fallback
	}

	opts := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s", lowerDir, layerDir, workDir)
	if err := syscall.Mount("overlay", mergedDir, "overlay", 0, opts); err != nil {
		// Fallback: just run in the layer dir directly
		fmt.Fprintf(os.Stderr, "  overlay mount failed, running in layer dir: %v\n", err)
		mergedDir = layerDir
	} else {
		defer func() {
			_ = syscall.Unmount(mergedDir, syscall.MNT_DETACH)
			_ = os.RemoveAll(mergedDir)
			_ = os.RemoveAll(workDir)
		}()
	}

	// Run command using unshare for isolation
	wd := b.workDir
	if wd == "" {
		wd = "/"
	}
	cmdArgs := []string{
		"--mount", "--pid", "--fork",
		"--root", mergedDir,
		"--wd", wd,
		"--",
	}
	cmdArgs = append(cmdArgs, instr.Args...)

	cmd := exec.Command("unshare", cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = b.buildEnv

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("RUN failed: %w", err)
	}

	b.layerDirs = append([]string{layerDir}, b.layerDirs...)
	fmt.Printf(" ---> Layer created\n")
	return nil
}

func (b *builder) handleCopy(instr Instruction) error {
	if len(instr.Args) < 2 {
		return fmt.Errorf("COPY requires at least 2 arguments")
	}

	layerDir, err := b.newLayer("copy")
	if err != nil {
		return err
	}

	src := instr.Args[:len(instr.Args)-1]
	dst := instr.Args[len(instr.Args)-1]

	// Resolve destination relative to WORKDIR
	if !filepath.IsAbs(dst) && b.workDir != "" {
		dst = filepath.Join(b.workDir, dst)
	}

	targetDir := filepath.Join(layerDir, dst)

	for _, s := range src {
		srcPath := filepath.Join(b.contextDir, s)
		fmt.Printf(" ---> COPY %s -> %s\n", s, dst)

		if err := copyPath(srcPath, targetDir); err != nil {
			return fmt.Errorf("copy %s: %w", s, err)
		}
	}

	b.layerDirs = append([]string{layerDir}, b.layerDirs...)
	return nil
}

func (b *builder) handleEnv(instr Instruction) error {
	for _, kv := range instr.Args {
		b.buildEnv = append(b.buildEnv, kv)
		fmt.Printf(" ---> ENV %s\n", kv)
	}
	return nil
}

func (b *builder) handleWorkdir(instr Instruction) error {
	b.workDir = instr.Args[0]
	fmt.Printf(" ---> WORKDIR %s\n", b.workDir)

	// Create workdir in current layer
	layerDir, err := b.newLayer("workdir")
	if err != nil {
		return err
	}
	target := filepath.Join(layerDir, b.workDir)
	if err := os.MkdirAll(target, 0755); err != nil {
		return err
	}
	b.layerDirs = append([]string{layerDir}, b.layerDirs...)
	return nil
}

func (b *builder) handleArg(instr Instruction) {
	if len(instr.Args) == 0 {
		return
	}
	arg := instr.Args[0]
	if strings.Contains(arg, "=") {
		parts := strings.SplitN(arg, "=", 2)
		b.buildArgs[parts[0]] = parts[1]
	}
}

func (b *builder) saveImage() error {
	name, tag := parseRef(b.cfg.Tag)
	dirName := name + "_" + tag
	imageDir := filepath.Join(mydockerRoot, "images", dirName)
	layersDir := filepath.Join(imageDir, "layers")

	if err := os.MkdirAll(layersDir, 0755); err != nil {
		return err
	}

	// Only save layers that are in /tmp (our build layers, not base image layers)
	buildLayerIdx := 0
	for _, layerDir := range b.layerDirs {
		if !strings.HasPrefix(layerDir, "/tmp/mydocker-build-") {
			// This is a base image layer — symlink or skip
			continue
		}
		destLayer := filepath.Join(layersDir, fmt.Sprintf("build%03d", buildLayerIdx))
		if err := copyDir(layerDir, destLayer); err != nil {
			return fmt.Errorf("save layer %d: %w", buildLayerIdx, err)
		}
		buildLayerIdx++
	}

	// Include base image layers directly in lowerdir list via a ref file
	// Instead of copying, we store the base layers path in a special file
	baseName, baseTag := parseRef(b.df.Instructions[0].Args[0])
	if baseTag == "" {
		baseTag = "latest"
	}
	baseDirName := baseName + "_" + baseTag
	baseLayersDir := filepath.Join(mydockerRoot, "images", baseDirName, "layers")
	// Write base layers reference
	_ = os.WriteFile(filepath.Join(imageDir, "base_layers"), []byte(baseLayersDir), 0644)

	// Save image config
	cfg := image.ImageConfig{
		Entrypoint: b.entrypoint,
		Cmd:        b.cmd,
		Env:        b.buildEnv,
		WorkingDir: b.workDir,
	}
	cfgData, _ := json.MarshalIndent(cfg, "", "  ")
	_ = os.WriteFile(filepath.Join(imageDir, "config.json"), cfgData, 0644)

	// Cleanup temp build dirs
	b.cleanupTempDirs()

	fmt.Printf("\nSuccessfully built %s\n", b.cfg.Tag)
	return nil
}

func (b *builder) newLayer(name string) (string, error) {
	dir, err := os.MkdirTemp("/tmp", "mydocker-build-"+name+"-*")
	if err != nil {
		return "", err
	}
	return dir, nil
}

func (b *builder) cleanupTempDirs() {
	for _, d := range b.layerDirs {
		if strings.HasPrefix(d, "/tmp/mydocker-build-") {
			_ = os.RemoveAll(d)
		}
	}
}

// getBaseLayers returns layer dirs for a base image
func getBaseLayers(layersDir string) []string {
	entries, err := os.ReadDir(layersDir)
	if err != nil {
		return nil
	}
	dirs := make([]string, len(entries))
	for i, e := range entries {
		dirs[len(entries)-1-i] = filepath.Join(layersDir, e.Name())
	}
	return dirs
}

func parseRef(ref string) (name, tag string) {
	parts := strings.SplitN(ref, ":", 2)
	name = parts[0]
	tag = "latest"
	if len(parts) == 2 {
		tag = parts[1]
	}
	return
}

// copyPath copies file or directory
func copyPath(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDir(src, dst)
	}
	return copyFile(src, dst)
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, _ := in.Stat()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
