package image

import (
	"encoding/json"
	"fmt"
	"strings"
	"os"
	"path/filepath"
	"text/tabwriter"
)

const imagesRoot = "/var/lib/mydocker/images"

// List hiển thị các images đã pull
func List() error {
	entries, err := os.ReadDir(imagesRoot)
	if os.IsNotExist(err) {
		fmt.Println("No images found.")
		return nil
	}
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "REPOSITORY\tTAG\tLAYERS\tSIZE")

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		ref := e.Name() // "alpine_3.18"
		// Convert dir name back to image ref: alpine_3.18 -> alpine:3.18
		imageRef := strings.Replace(ref, "_", ":", 1)
		name, tag := parseImageRef(imageRef)

		// Đếm layers
		layersDir := filepath.Join(imagesRoot, ref, "layers")
		layerCount := 0
		size := int64(0)
		if layerEntries, err := os.ReadDir(layersDir); err == nil {
			layerCount = len(layerEntries)
			// Tính tổng size của layers
			filepath.Walk(layersDir, func(path string, info os.FileInfo, err error) error {
				if err == nil && !info.IsDir() {
					size += info.Size()
				}
				return nil
			})
		}

		// Đọc manifest nếu có
		manifestPath := filepath.Join(imagesRoot, ref, "manifest.json")
		if layerCount == 0 {
			if data, err := os.ReadFile(manifestPath); err == nil {
				var m Manifest
				if json.Unmarshal(data, &m) == nil {
					layerCount = len(m.Layers)
				}
			}
		}

		fmt.Fprintf(w, "%s\t%s\t%d\t%s\n",
			name, tag, layerCount, formatSize(size))
	}

	return w.Flush()
}

func formatSize(bytes int64) string {
	switch {
	case bytes >= 1024*1024*1024:
		return fmt.Sprintf("%.1f GB", float64(bytes)/1024/1024/1024)
	case bytes >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(bytes)/1024/1024)
	case bytes >= 1024:
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
