package image

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	registryBase = "https://registry-1.docker.io/v2"
	authBase     = "https://auth.docker.io/token"
)

// ImageConfig holds parsed image configuration
type ImageConfig struct {
	Entrypoint []string `json:"Entrypoint"`
	Cmd        []string `json:"Cmd"`
	Env        []string `json:"Env"`
	WorkingDir string   `json:"WorkingDir"`
	User       string   `json:"User"`
}

// Pull downloads image from Docker Hub
func Pull(imageRef string) error {
	name, tag := parseImageRef(imageRef)
	fmt.Printf("Pulling %s:%s from Docker Hub...\n", name, tag)

	token, err := getToken(name)
	if err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	fmt.Println("Fetching manifest...")
	manifest, err := getManifest(name, tag, token)
	if err != nil {
		return fmt.Errorf("manifest: %w", err)
	}

	// Use _ instead of : in dir name (colon breaks overlayfs lowerdir syntax)
	imageDir := filepath.Join("/var/lib/mydocker/images", name+"_"+tag)
	if err := os.MkdirAll(imageDir, 0755); err != nil {
		return err
	}

	// Save manifest
	manifestData, _ := json.MarshalIndent(manifest, "", "  ")
	_ = os.WriteFile(filepath.Join(imageDir, "manifest.json"), manifestData, 0644)

	// Download image config blob (contains Entrypoint, Cmd, Env, WorkingDir)
	fmt.Println("Fetching image config...")
	if err := downloadConfig(name, manifest.Config.Digest, token, imageDir); err != nil {
		fmt.Fprintf(os.Stderr, "warning: config download failed: %v\n", err)
	}

	// Download layers
	fmt.Printf("Pulling %d layers...\n", len(manifest.Layers))
	for i, layer := range manifest.Layers {
		fmt.Printf("  Layer %d/%d: %s...\n", i+1, len(manifest.Layers), layer.Digest[:19])
		layerDir := filepath.Join(imageDir, "layers", sanitizeDigest(layer.Digest))
		if _, err := os.Stat(layerDir); err == nil {
			fmt.Printf("    Already exists, skipping\n")
			continue
		}
		if err := downloadLayer(name, layer.Digest, token, layerDir); err != nil {
			return fmt.Errorf("download layer %s: %w", layer.Digest[:19], err)
		}
	}

	fmt.Printf("Successfully pulled %s:%s\n", name, tag)
	return nil
}

// Manifest structure
type Manifest struct {
	SchemaVersion int    `json:"schemaVersion"`
	MediaType     string `json:"mediaType"`
	Config        struct {
		Digest string `json:"digest"`
		Size   int    `json:"size"`
	} `json:"config"`
	Layers []struct {
		Digest    string `json:"digest"`
		Size      int    `json:"size"`
		MediaType string `json:"mediaType"`
	} `json:"layers"`
	Manifests []struct {
		Digest   string `json:"digest"`
		Platform struct {
			OS           string `json:"os"`
			Architecture string `json:"architecture"`
		} `json:"platform"`
	} `json:"manifests"`
}

// rawImageConfig is the full image config blob structure
type rawImageConfig struct {
	Config ImageConfig `json:"config"`
}

func downloadConfig(imageName, digest, token, imageDir string) error {
	repo := imageName
	if !strings.Contains(repo, "/") {
		repo = "library/" + repo
	}

	url := fmt.Sprintf("%s/%s/blobs/%s", registryBase, repo, digest)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// Parse and save simplified config
	var raw rawImageConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	configData, err := json.MarshalIndent(raw.Config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(imageDir, "config.json"), configData, 0644)
}

// LoadImageConfig reads the saved image config
func LoadImageConfig(imageName string) (*ImageConfig, error) {
	name, tag := parseImageRef(imageName)
	dirName := name + "_" + tag
	configPath := filepath.Join("/var/lib/mydocker/images", dirName, "config.json")

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var cfg ImageConfig
	return &cfg, json.Unmarshal(data, &cfg)
}

func getToken(imageName string) (string, error) {
	repo := imageName
	if !strings.Contains(repo, "/") {
		repo = "library/" + repo
	}
	url := fmt.Sprintf("%s?service=registry.docker.io&scope=repository:%s:pull", authBase, repo)
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.Token, nil
}

func getManifest(name, tag, token string) (*Manifest, error) {
	repo := name
	if !strings.Contains(repo, "/") {
		repo = "library/" + repo
	}
	url := fmt.Sprintf("%s/%s/manifests/%s", registryBase, repo, tag)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.docker.distribution.manifest.v2+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var m Manifest
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, err
	}
	if len(m.Manifests) > 0 {
		for _, entry := range m.Manifests {
			if entry.Platform.OS == "linux" && entry.Platform.Architecture == "amd64" {
				return getManifestByDigest(repo, entry.Digest, token)
			}
		}
		return nil, fmt.Errorf("no linux/amd64 manifest found")
	}
	return &m, nil
}

func getManifestByDigest(repo, digest, token string) (*Manifest, error) {
	url := fmt.Sprintf("%s/%s/manifests/%s", registryBase, repo, digest)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var m Manifest
	return &m, json.NewDecoder(resp.Body).Decode(&m)
}

func downloadLayer(imageName, digest, token, destDir string) error {
	repo := imageName
	if !strings.Contains(repo, "/") {
		repo = "library/" + repo
	}
	url := fmt.Sprintf("%s/%s/blobs/%s", registryBase, repo, digest)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	hasher := sha256.New()
	reader := io.TeeReader(resp.Body, hasher)
	if err := extractTarGz(reader, destDir); err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	got := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
	if got != digest {
		os.RemoveAll(destDir)
		return fmt.Errorf("digest mismatch: got %s, want %s", got[:19], digest[:19])
	}
	return nil
}

func extractTarGz(r io.Reader, destDir string) error {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(hdr.Name)
		if filepath.IsAbs(name) {
			name = name[1:]
		}
		target := filepath.Join(destDir, name)
		base := filepath.Base(name)
		if strings.HasPrefix(base, ".wh.") {
			whiteoutTarget := filepath.Join(filepath.Dir(target), base[4:])
			_ = os.RemoveAll(whiteoutTarget)
			continue
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			_ = os.MkdirAll(target, os.FileMode(hdr.Mode))
		case tar.TypeReg, tar.TypeRegA:
			_ = os.MkdirAll(filepath.Dir(target), 0755)
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				continue
			}
			_, _ = io.Copy(f, tr)
			f.Close()
		case tar.TypeSymlink:
			_ = os.MkdirAll(filepath.Dir(target), 0755)
			_ = os.Symlink(hdr.Linkname, target)
		case tar.TypeLink:
			linkTarget := filepath.Join(destDir, filepath.Clean(hdr.Linkname))
			_ = os.MkdirAll(filepath.Dir(target), 0755)
			_ = os.Link(linkTarget, target)
		}
	}
	return nil
}

func parseImageRef(ref string) (name, tag string) {
	parts := strings.SplitN(ref, ":", 2)
	name = parts[0]
	tag = "latest"
	if len(parts) == 2 {
		tag = parts[1]
	}
	return
}

func sanitizeDigest(digest string) string {
	if strings.HasPrefix(digest, "sha256:") {
		return digest[7:19]
	}
	return digest[:12]
}
