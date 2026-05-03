# Design Decisions và Trade-offs

Tài liệu này ghi lại các quyết định kiến trúc quan trọng trong mydocker — tại sao chọn cách này thay vì cách khác, trade-off là gì, và những edge case cần biết.

---

## 1. Reexec pattern thay vì pre-setup

### Quyết định

Child process exec lại chính binary với argument `init` thay vì setup namespace từ parent rồi exec thẳng lệnh user.

### Lý do kỹ thuật

`clone(CLONE_NEWNS)` tạo namespace mới tại thời điểm fork — nhưng một số setup (overlayfs mount, pivot_root) **phải chạy bên trong namespace mới** sau khi nó được tạo. Không thể setup trước fork vì namespace chưa tồn tại, không thể setup sau exec vì process đã bị thay thế.

Reexec là pattern chuẩn của runc, Docker, containerd:

```
Parent:  clone(flags) → child trong namespace mới
Child:   exec /proc/self/exe "init" → setup rootfs → exec user command
```

`/proc/self/exe` là symlink đến binary đang chạy — portable hơn hardcode path.

### Alternative bị loại

**`nsenter` từ parent**: Có thể dùng `nsenter` để enter namespace của child từ parent, nhưng tạo ra race condition giữa parent setup và child execution. Reexec đơn giản và deterministic hơn.

**`unshare` trong child**: Thay vì dùng `clone` flags, để child tự gọi `unshare()`. Không hoạt động vì `unshare(CLONE_NEWPID)` chỉ có hiệu lực với process **tiếp theo được fork**, không phải process hiện tại.

---

## 2. Shell out `ip` commands thay vì netlink library

### Quyết định

Network setup dùng `exec.Command("ip", ...)` thay vì `github.com/vishvananda/netlink`.

### Lý do

- **Dependency**: netlink library thêm dependency không nhỏ, cần CGO trên một số platform
- **Debugging**: `ip` commands dễ reproduce và debug hơn khi xảy ra lỗi
- **Scope**: mydocker là project học — readability quan trọng hơn performance
- **Correctness**: `ip` đã được test kỹ trên mọi kernel version

### Trade-off

Shell out có overhead fork/exec per operation (~1-2ms). Trong production container runtime (containerd), netlink được dùng để tránh overhead này — đặc biệt quan trọng khi start hàng trăm containers đồng thời.

### Khi nào nên dùng netlink

Nếu cần start >50 containers/giây hoặc implement advanced features (IPVLAN, MACVLAN, SR-IOV), netlink là bắt buộc.

---

## 3. Flat JSON files cho state thay vì database

### Quyết định

Container state lưu trong `state.json` per-container directory thay vì SQLite hoặc embedded database.

### Lý do

- **Simplicity**: không dependency, không migration, không schema
- **Debuggability**: `cat state.json` xem ngay, `jq` để query
- **Crash safety**: mỗi container state độc lập, một file corrupt không ảnh hưởng container khác
- **Atomic write**: ghi toàn bộ file một lần, không có partial update

### Trade-off

- **Query performance**: list containers cần scan tất cả directories — O(n) với n = số containers
- **No transactions**: nếu cần update nhiều containers atomically (compose down), có thể inconsistent nếu crash giữa chừng
- **No indexing**: tìm container theo name cần scan tất cả state files

### Tại sao không SQLite

Docker dùng boltdb (embedded key-value store) vì cần transaction và atomic batch updates khi quản lý hàng nghìn containers. Ở scale của mydocker, JSON files đủ dùng và đơn giản hơn nhiều.

---

## 4. HTTP/Unix socket API thay vì gRPC

### Quyết định

Daemon expose HTTP REST API qua Unix socket thay vì gRPC (như containerd).

### Lý do

- **Tooling**: có thể test với `curl --unix-socket` không cần client library
- **Simplicity**: không cần protobuf, không code generation
- **Compatibility**: dễ implement client bằng bất kỳ ngôn ngữ nào
- **Debugging**: Wireshark/tcpdump đọc được HTTP, không đọc được binary protobuf

### Trade-off

- **Performance**: HTTP có overhead parsing, gRPC binary protocol hiệu quả hơn ~30-40%
- **Streaming**: gRPC có streaming native (dùng cho `docker logs -f`, `docker stats`). HTTP cần SSE hoặc WebSocket
- **Type safety**: protobuf có schema validation, JSON không có

### Tại sao containerd dùng gRPC

containerd quản lý container cho toàn bộ Kubernetes cluster — performance và type safety là bắt buộc. mydocker không có use case đó.

---

## 5. Single upper layer thay vì snapshot chain

### Quyết định

Mỗi container có một `upper/` directory duy nhất thay vì snapshot chain như containerd/snapshotter.

### Cách containerd làm

```
Image layers:  L1 ← L2 ← L3
Container A:   L1 ← L2 ← L3 ← snapshot_A
Container B:   L1 ← L2 ← L3 ← snapshot_B
Commit A:      L1 ← L2 ← L3 ← snapshot_A ← snapshot_commit
```

Snapshot chain cho phép commit container thành image mới và build incremental layers.

### mydocker làm

```
Image layers:  L1:L2:L3 (overlayfs lowerdir)
Container A:   upper_A  (một directory duy nhất)
```

### Trade-off

**Mất:**
- Không thể `mydocker commit` container thành image mới
- Không hỗ trợ incremental build cache cho Dockerfile
- Không thể share writeable layer giữa containers

**Giữ được:**
- Đơn giản hơn nhiều — không cần snapshot manager
- overlayfs lowerdir chain vẫn hoạt động đúng cho read
- Đủ cho use case run container

---

## 6. image dir naming: `alpine_latest` thay vì `alpine:latest`

### Quyết định

Image directory dùng `_` thay vì `:` trong tên: `/var/lib/mydocker/images/alpine_latest/`.

### Lý do kỹ thuật

overlayfs `lowerdir` option dùng `:` làm separator giữa các layers:

```
lowerdir=/path/layer3:/path/layer2:/path/layer1
```

Nếu path chứa `:` (như `/images/alpine:latest/layers/abc`), kernel parse sai — `alpine` là một lowerdir, `latest/layers/abc` là lowerdir khác. Đây là một pitfall được ghi trong overlayfs documentation.

### Implication

Code phải normalize khi lưu và khi đọc:

```go
// Lưu
dirName := strings.ReplaceAll(imageRef, ":", "_")  // alpine:3.18 → alpine_3.18

// Đọc lại cho user
imageRef := strings.Replace(dirName, "_", ":", 1)  // alpine_3.18 → alpine:3.18
```

---

## 7. `pivot_root` thay vì `chroot`

### Quyết định

Dùng `pivot_root` syscall thay vì `chroot` để thay đổi root filesystem của container.

### Sự khác biệt kỹ thuật

`chroot` chỉ thay đổi "concept" của `/` cho process — kernel vẫn giữ reference đến real root. Với đủ privilege, process có thể escape:

```c
// Classic chroot escape
mkdir("escape");
chdir("escape");
chroot(".");           // chroot vào subdir
for (int i = 0; i < 100; i++) chdir("..");  // đi lên đến real root
// Bây giờ CWD là real root dù đang "trong" chroot
```

`pivot_root` thay thế toàn bộ root mount point trong mount namespace — old root bị unmount hoàn toàn, không có reference nào còn lại.

### Prerequisite của `pivot_root`

1. Process phải có CAP_SYS_ADMIN
2. New root phải là một mount point (không phải directory thường) → cần bind mount trước
3. New root và put_old phải trên **cùng filesystem khác** với current root
4. Phải trong mount namespace riêng (không thể pivot_root trong host namespace)

Điều kiện 3 là lý do phải bind mount `mergedDir` vào chính nó trước khi `pivot_root`:

```go
syscall.Mount(mergedDir, mergedDir, "", syscall.MS_BIND|syscall.MS_REC, "")
```

---

## 8. Volume mount trước pivot_root thay vì sau

### Quyết định

Bind mount volumes vào `mergedDir` **trước** khi `pivot_root`, không phải sau.

### Lý do

Sau `pivot_root`, host filesystem không còn accessible. Path `/tmp/mydata` trên host trở thành path không tồn tại trong container's view của filesystem.

Thứ tự đúng:

```
1. Mount overlayfs → mergedDir
2. Bind mount /host/path → mergedDir/container/path  ← host path còn accessible
3. pivot_root(mergedDir)
4. Mount /proc /sys /dev  ← container paths, không cần host path
```

### Edge case

Volume target directory phải tồn tại trong `mergedDir` trước khi bind mount. Nếu container image không có `/data` directory, bind mount sẽ fail với "no such file or directory". mydocker tạo target directory trước:

```go
target := filepath.Join(mergedDir, v.ContainerPath)
os.MkdirAll(target, 0755)
syscall.Mount(v.HostPath, target, "", MS_BIND|MS_REC, "")
```

---

## 9. Image config resolution — tại sao cần download riêng

### Quyết định

Pull image phải download cả config blob (không chỉ manifest và layers).

### Docker Registry API v2 structure

```
Manifest v2:
  config: {digest: sha256:abc, mediaType: "...image.config..."}
  layers: [{digest: sha256:def}, ...]

Config blob (sha256:abc):
  {
    "config": {
      "Entrypoint": [...],
      "Cmd": [...],
      "Env": [...],
      "WorkingDir": "..."
    },
    "rootfs": {"diff_ids": [...]}
  }
```

Manifest chỉ chứa digest của layers và config — không chứa Entrypoint/Cmd trực tiếp. Phải download config blob riêng.

Nếu không download config, `mydocker run nginx:latest` không biết cần chạy `/docker-entrypoint.sh nginx -g "daemon off;"` → container exit ngay.

### Multi-arch manifest list

Docker Hub trả về manifest list (application/vnd.docker.distribution.manifest.list.v2+json) thay vì manifest trực tiếp khi image support nhiều architecture:

```json
{
  "manifests": [
    {"digest": "sha256:xxx", "platform": {"os": "linux", "architecture": "amd64"}},
    {"digest": "sha256:yyy", "platform": {"os": "linux", "architecture": "arm64"}}
  ]
}
```

mydocker phải detect manifest list và resolve đúng manifest cho `linux/amd64`:

```go
if len(m.Manifests) > 0 {
    for _, entry := range m.Manifests {
        if entry.Platform.OS == "linux" && entry.Platform.Architecture == "amd64" {
            return getManifestByDigest(repo, entry.Digest, token)
        }
    }
}
```

---

## 10. Daemon restart recovery

### Quyết định

Khi daemon khởi động lại, containers đang chạy được "recovered" — daemon check PID còn sống và cập nhật state, không restart chúng.

### Vấn đề

Containers được fork bởi daemon process. Khi daemon crash/restart, containers vẫn chạy (vì chúng là separate processes), nhưng daemon mất track.

### Giải pháp

```go
func (d *Daemon) recoverContainers() {
    // Đọc tất cả state.json
    // Với mỗi container có status "running":
    //   Kiểm tra PID còn sống không: kill(pid, 0) == nil
    //   Nếu còn: giữ nguyên state
    //   Nếu không: update status → "exited"
}
```

### Limitation

Sau recovery, daemon không có reference đến `exec.Cmd` của container → không thể `cmd.Wait()` để detect khi container exit và trigger restart policy. Restart policy chỉ hoạt động với containers được start bởi **instance hiện tại** của daemon.

Production solution (containerd): lưu shim process (separate process per container) để tồn tại qua daemon restart.
