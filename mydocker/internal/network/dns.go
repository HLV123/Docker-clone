package network

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const dnsFile = "/var/lib/mydocker/dns/hosts"

var dnsMu sync.Mutex

// RegisterContainer adds a container name -> IP mapping
// Can be called multiple times for same container with different names
func RegisterContainer(name, containerID, ip string) error {
	if name == "" || ip == "" {
		return nil
	}
	dnsMu.Lock()
	defer dnsMu.Unlock()

	if err := os.MkdirAll(filepath.Dir(dnsFile), 0755); err != nil {
		return err
	}

	entries := readDNSEntries()

	// Remove old entry with same name (avoid duplicates)
	newEntries := []string{}
	for _, e := range entries {
		fields := strings.Fields(e)
		if len(fields) >= 2 && fields[1] == name {
			continue // remove duplicate name
		}
		newEntries = append(newEntries, e)
	}

	// Add new entry
	newEntries = append(newEntries, fmt.Sprintf("%s\t%s\t#%s", ip, name, containerID))

	return os.WriteFile(dnsFile, []byte(strings.Join(newEntries, "\n")+"\n"), 0644)
}

// UnregisterContainer removes all DNS entries for a container
func UnregisterContainer(containerID string) {
	dnsMu.Lock()
	defer dnsMu.Unlock()

	entries := readDNSEntries()
	newEntries := []string{}
	for _, e := range entries {
		if !strings.Contains(e, "#"+containerID) {
			newEntries = append(newEntries, e)
		}
	}
	_ = os.WriteFile(dnsFile, []byte(strings.Join(newEntries, "\n")+"\n"), 0644)
}

// InjectDNS writes the DNS hosts file into a container's rootfs
func InjectDNS(mergedDir string) {
	// Copy our custom hosts file into container
	data, err := os.ReadFile(dnsFile)
	if err != nil {
		// No DNS entries yet, use default
		data = []byte("127.0.0.1\tlocalhost\n::1\tlocalhost\n")
	}

	// Read host resolv.conf for upstream DNS
	resolvConf, _ := os.ReadFile("/etc/resolv.conf")

	hostsPath := filepath.Join(mergedDir, "etc", "hosts")
	_ = os.MkdirAll(filepath.Join(mergedDir, "etc"), 0755)

	// Merge: default + container DNS entries
	hosts := "127.0.0.1\tlocalhost\n::1\tlocalhost\n" + string(data)
	_ = os.WriteFile(hostsPath, []byte(hosts), 0644)

	// Write resolv.conf
	if resolvConf != nil {
		_ = os.WriteFile(filepath.Join(mergedDir, "etc", "resolv.conf"), resolvConf, 0644)
	}
}

func readDNSEntries() []string {
	data, err := os.ReadFile(dnsFile)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	result := []string{}
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			result = append(result, l)
		}
	}
	return result
}

// GetContainerIP looks up IP by container name
func GetContainerIP(name string) string {
	dnsMu.Lock()
	defer dnsMu.Unlock()

	entries := readDNSEntries()
	for _, e := range entries {
		fields := strings.Fields(e)
		if len(fields) >= 2 && fields[1] == name {
			return fields[0]
		}
	}
	return ""
}
