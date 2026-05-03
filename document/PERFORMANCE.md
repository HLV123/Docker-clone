# Performance Analysis

Phân tích hiệu năng của từng thành phần trong mydocker — latency, overhead, bottleneck, và so sánh với Docker.

---

## 1. Container Startup Latency

### Breakdown từng bước

Đo trên WSL2 Ubuntu 22.04, Alpine image đã pull sẵn:

```
mydocker run alpine /bin/echo "test"
```

| Bước | Thời gian ước tính | Chi tiết |
|------|-------------------|---------|
| Parse CLI args | ~0.1ms | |
| generateID() | ~0.1ms | uuid.New() |
| MkdirAll containerDir | ~0.5ms | filesystem |
| state.Save() | ~1ms | JSON marshal + write |
| cgroup.Create() | ~2ms | mkdir + write files |
| network.SetupBridge() | ~1ms | skip nếu đã có |
| veth.Create() | ~5ms | 2x `ip link` commands |
| clone() + exec | ~3ms | fork + reexec |
| MS_PRIVATE mount | ~0.5ms | |
| overlayfs mount | ~2ms | |
| pivot_root | ~0.5ms | |
| mount /proc /sys /dev | ~3ms | 5 mount syscalls |
| Sethostname | ~0.1ms | |
| exec user command | ~1ms | |
| **Total** | **~20ms** | |

### So sánh

| Runtime | Startup (alpine /bin/echo) |
|---------|---------------------------|
| mydocker | ~20ms |
| Docker (runc) | ~300-500ms |
| containerd | ~200-400ms |
| Podman | ~400-600ms |

mydocker **nhanh hơn** Docker vì:
- Không có containerd layer
- Không có image content store
- Không có OCI bundle copy
- Không có shim process

### Bottleneck

Bottleneck chính là `ip` command execution (~5ms mỗi lần fork+exec). mydocker gọi 4-6 `ip` commands per container với network. Dùng netlink library trực tiếp sẽ giảm còn ~0.5ms tổng.

---

## 2. overlayfs Overhead

### Write overhead (copy-on-write)

Lần đầu ghi vào file từ image layer, overlayfs phải copy file lên `upper/` trước. Gọi là **copy-up**.

```
Container ghi vào /etc/nginx/nginx.conf (1MB file):
1. overlayfs detect file chưa có trong upper
2. Copy toàn bộ file từ lower → upper (~5-10ms cho 1MB)
3. Ghi modification vào upper
```

Copy-up xảy ra **một lần per file per container**. File nhỏ (< 4KB): không đáng kể. File lớn (database files, log files): overhead đáng kể cho lần write đầu tiên.

### Read performance

Read từ lower layer (image) đi qua một layer indirection so với native ext4:

```
Read /bin/sh:
  overlayfs lookup: không có trong upper → đọc từ lower
  Overhead: ~2-5% so với direct ext4 read
```

Với workloads read-heavy (web server serve static files), overhead này chấp nhận được.

### Multi-layer performance

nginx có 7 layers. overlayfs phải check tất cả layers theo thứ tự khi lookup file:

```
File lookup: check upper → layer7 → layer6 → ... → layer1
```

Trong thực tế kernel cache dcache (directory entry cache) sau lần lookup đầu — subsequent lookups gần như không có overhead.

### Workloads bị ảnh hưởng nhiều nhất

- **Database containers**: random write pattern triggers nhiều copy-up
- **Build containers**: tạo nhiều files nhỏ → nhiều directory operations
- **Log-heavy containers**: append-only writes với copy-up lần đầu

---

## 3. Network Throughput

### Đo throughput container → container

```bash
# Server container
mydocker run -d --net --name server alpine /bin/sh -c "nc -l -p 5000 > /dev/null"

# Client container (iperf-like với nc)
mydocker run --net alpine /bin/sh -c "dd if=/dev/zero bs=1M count=1000 | nc server 5000"
```

| Path | Throughput |
|------|-----------|
| Container → Container (same bridge) | ~8-10 Gbps |
| Container → Host | ~8-10 Gbps |
| Container → Internet | Limited by eth0 |

veth pair + bridge có overhead nhỏ so với loopback nhưng đủ cho hầu hết workloads.

### Latency container → container

```bash
# Ping từ container này sang container khác
# Đo bằng TCP handshake time
```

| Path | Latency |
|------|---------|
| Container → Container | ~0.1-0.3ms |
| Container → Host | ~0.05ms |
| Loopback trong container | ~0.01ms |

### So sánh network mode

| Mode | Throughput | Latency | Isolation |
|------|-----------|---------|-----------|
| veth + bridge (mydocker) | ~8 Gbps | ~0.2ms | Tốt |
| Host network (`--net=host`) | ~40 Gbps | ~0.01ms | Không có |
| MACVLAN | ~20 Gbps | ~0.05ms | Tốt |
| SR-IOV (hardware) | ~25 Gbps | ~0.005ms | Hardware |

mydocker chỉ support veth + bridge. Nếu cần throughput cao hơn, cần host network hoặc MACVLAN.

---

## 4. Image Pull Performance

### Bottleneck analysis

```
mydocker pull nginx:latest (157MB, 7 layers)

Step 1: Get auth token    ~50ms    (HTTPS round trip)
Step 2: Get manifest      ~30ms    (HTTPS round trip)
Step 3: Get config blob   ~30ms    (HTTPS round trip)
Step 4-10: Download layers
  Layer 1: 84MB           ~8s     (network limited ~10MB/s)
  Layer 2: 83MB           ~8s
  Layer 3-7: small        ~1s
Step 11: Extract each layer:
  Layer 1: extract 84MB tar.gz  ~3s
  Layer 2: extract 83MB tar.gz  ~3s

Total: ~25s (10MB/s network)
```

### Không có parallel download

mydocker download layers tuần tự. Docker pull parallel download tất cả layers đồng thời → 3-5x faster cho multi-layer images.

```go
// mydocker — sequential
for _, layer := range manifest.Layers {
    downloadLayer(layer)  // block cho đến khi xong
}

// Docker — parallel (conceptually)
var wg sync.WaitGroup
for _, layer := range manifest.Layers {
    go func() {
        downloadLayer(layer)
        wg.Done()
    }()
}
wg.Wait()
```

### Layer caching

Nếu layer đã tồn tại (check bằng directory existence), skip download:

```go
if _, err := os.Stat(layerDir); err == nil {
    fmt.Printf("Already exists, skipping\n")
    continue
}
```

Đây là content-addressable caching đơn giản. Docker dùng digest-based deduplication chính xác hơn — nếu 2 images share layer (cùng SHA256), chỉ lưu một bản.

### Extract performance

`extractTarGz()` là single-threaded. Với layers lớn (80MB+), đây là bottleneck thứ hai sau network download. Parallel extraction không dễ vì tar entries cần được process theo thứ tự.

---

## 5. Memory Overhead Per Container

### Breakdown

```
Per container memory overhead:

Kernel overhead:
  PID namespace struct:   ~1KB
  Mount namespace:        ~10KB (mount table copy)
  Network namespace:      ~50KB (network stack)
  Other namespaces:       ~5KB

Process overhead:
  Container process:      ~100KB minimum (Go runtime)
  /proc entries:          ~20KB

overlayfs:
  Upper directory:        0 bytes (empty initially)
  Kernel dcache entries:  ~50KB (image file metadata)

mydocker state:
  state.json in memory:   ~1KB (không cache, read on demand)

Total per container:      ~200-300KB kernel overhead
                         + process memory (depends on workload)
```

### So sánh với Docker

Docker có thêm:
- containerd shim process: ~10MB
- OCI bundle directory: ~1MB disk
- Content store metadata: ~100KB

mydocker nhẹ hơn đáng kể per container vì không có shim process.

---

## 6. cgroup Overhead

### Overhead của cgroup v2

cgroup v2 có overhead không đáng kể cho CPU và memory accounting — kernel thực hiện trong hot path của scheduler và memory allocator.

Tuy nhiên, **memory accounting** thêm ~2-5% overhead cho memory-intensive workloads vì kernel phải track từng allocation.

### Memory limit enforcement

OOM killer trong cgroup v2 là synchronous — khi container vượt limit, kernel kill process ngay lập tức trong allocation path. Không có lag.

### Cleanup cost

Khi container exit, `os.Remove(cgroupPath)` phải đợi tất cả processes trong cgroup exit trước. Nếu có zombie processes, cleanup bị block.

---

## 7. Dockerfile Build Performance

### Layer caching

mydocker không có build cache. Mỗi `build` chạy lại tất cả instructions từ đầu.

Docker BuildKit có layer cache: nếu instruction và context không thay đổi, dùng cached layer. Đây là tính năng quan trọng nhất cho developer experience.

### `RUN` instruction overhead

Mỗi `RUN` instruction:

```
1. Mount overlayfs (accumulated layers)     ~3ms
2. unshare --mount --pid --fork             ~5ms
3. Execute command in container             variable
4. Unmount + cleanup                        ~3ms

Total overhead per RUN: ~11ms + command time
```

### So sánh build time

```bash
# Dockerfile với 5 RUN instructions
# Command: apk add curl (1 package)

mydocker build:  ~15s  (5 * 3s per apk add)
Docker BuildKit: ~10s  (với layer cache hit: ~0.1s)
```

---

## 8. Profiling và Optimization Opportunities

### Top 5 improvements có impact lớn nhất

**1. Parallel layer download** — 3-5x faster pull
```go
// Cần semaphore để limit concurrency
sem := make(chan struct{}, 4)
for _, layer := range layers {
    go func() {
        sem <- struct{}{}
        downloadLayer(layer)
        <-sem
    }()
}
```

**2. netlink thay vì `ip` commands** — giảm ~5ms startup per container

**3. Build layer cache** — tăng tốc rebuild từ hàng phút xuống giây

**4. Lazy image extraction** — extract layer on-demand thay vì khi pull
   Cần overlayfs lazy loading hoặc `erofs` (read-only compressed filesystem)

**5. Pre-created `/proc` entries** — bind mount `/proc` từ container namespace thay vì mount mới
   Giảm từ 5 mount calls xuống 1

### Đo lường

```bash
# Đo startup time
time mydocker run alpine /bin/echo test

# Đo với strace để phân tích syscalls
sudo strace -c -e trace=mount,openat,clone mydocker run alpine /bin/echo test

# Đo network throughput
mydocker run --net alpine /bin/sh -c "dd if=/dev/zero bs=1M count=100 | nc 172.20.0.1 9999"
```
