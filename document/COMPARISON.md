# So sánh với Docker, containerd, runc

Phân tích chi tiết sự khác biệt giữa mydocker và production container runtimes về kiến trúc, compliance, và feature parity.

---

## 1. Kiến trúc tổng quan

### Docker Engine stack

```
docker CLI
    │  HTTPS/Unix socket (Docker API)
    ▼
dockerd (Docker Engine)
    │  gRPC
    ▼
containerd
    │  gRPC (CRI — Container Runtime Interface)
    ▼
containerd-shim-runc-v2  ← một process per container, tồn tại qua daemon restart
    │
    ▼
runc  ← OCI runtime, fork + exec, exit sau khi container start
    │
    ▼
container process
```

### mydocker stack

```
mydocker CLI
    │  HTTP/Unix socket
    ▼
mydockerd
    │  direct function call (không có shim)
    ▼
container process  ← fork trực tiếp bởi daemon
```

### Sự khác biệt kiến trúc quan trọng nhất: shim process

containerd dùng `containerd-shim` — một process nhỏ (1 per container) tồn tại độc lập với containerd daemon:

```
containerd restart → shim vẫn chạy → container không bị ảnh hưởng
containerd lấy lại control qua shim socket
```

mydocker không có shim → nếu mydockerd crash, mất track của containers đang chạy (chúng vẫn chạy nhưng restart policy không hoạt động).

---

## 2. OCI Runtime Spec Compliance

### Spec: https://github.com/opencontainers/runtime-spec

| Section | mydocker | runc | Notes |
|---------|----------|------|-------|
| `ociVersion` | ✓ 1.0.2 | ✓ 1.2.1 | |
| `process.args` | ✓ | ✓ | |
| `process.env` | ✓ | ✓ | |
| `process.cwd` | ✓ | ✓ | |
| `process.terminal` | Partial | ✓ | TTY chưa hoàn chỉnh |
| `process.capabilities` | Minimal | ✓ | Không drop đúng |
| `process.rlimits` | Listed, not enforced | ✓ | |
| `process.noNewPrivileges` | Listed | ✓ | Không call `PR_SET_NO_NEW_PRIVS` |
| `root.path` | ✓ | ✓ | |
| `root.readonly` | ✗ | ✓ | |
| `mounts` | Partial | ✓ | Default mounts có, custom chưa đủ |
| `linux.namespaces` | ✓ | ✓ | |
| `linux.uidMappings` | Partial | ✓ | Rootless chưa hoàn chỉnh |
| `linux.resources.memory` | ✓ | ✓ | |
| `linux.resources.cpu` | ✓ | ✓ | |
| `linux.resources.pids` | ✓ | ✓ | |
| `linux.seccomp` | ✗ | ✓ | Không implement |
| `linux.maskedPaths` | ✓ (listed) | ✓ | Config có nhưng không apply |
| `linux.readonlyPaths` | ✓ (listed) | ✓ | Config có nhưng không apply |
| `hooks` | ✗ | ✓ | prestart, poststart, poststop |
| `annotations` | ✓ (stored) | ✓ | |

**Compliance level**: ~60% của OCI Runtime Spec 1.0.2

Spec đủ để generate valid `config.json` và chạy với runc — nhưng không đủ để implement `runc create` + `runc start` flow.

---

## 3. OCI Image Spec Compliance

### Spec: https://github.com/opencontainers/image-spec

| Feature | mydocker | Docker | Notes |
|---------|----------|--------|-------|
| Manifest v2 | ✓ | ✓ | |
| Manifest list (multi-arch) | ✓ | ✓ | Resolve linux/amd64 |
| Layer download + verify SHA256 | ✓ | ✓ | |
| Layer extraction (tar.gz) | ✓ | ✓ | |
| Whiteout files | Partial | ✓ | `.wh.` prefix handled, `.wh..wh..opq` (opaque whiteout) không |
| Hard links trong tar | ✓ | ✓ | |
| Symlinks trong tar | ✓ (no SecureJoin) | ✓ (SecureJoin) | Security gap |
| SUID/SGID bits preservation | Partial | ✓ | |
| Extended attributes (xattr) | ✗ | ✓ | |
| Image config (Entrypoint, Cmd, Env) | ✓ | ✓ | |
| Image config (User, ExposedPorts) | Stored, not enforced | ✓ | |
| Content addressable storage | Partial | ✓ | Layer by digest, không dedup |
| Image push | ✗ | ✓ | |
| Image tag | ✗ | ✓ | |
| Image build cache | ✗ | ✓ | |

### Opaque whiteout

Docker hỗ trợ `.wh..wh..opq` — đánh dấu toàn bộ directory trong layer dưới bị xóa (không chỉ file cụ thể). mydocker không xử lý trường hợp này — một số images có thể không hiển thị đúng filesystem.

---

## 4. Docker API Compatibility

Docker API (`/var/run/docker.sock`) là tiêu chuẩn de facto. Nhiều tools (Portainer, Lazydocker, VS Code Docker extension) dùng Docker API.

### mydocker API vs Docker API

| Endpoint | mydocker | Docker |
|----------|----------|--------|
| `GET /containers/json` | ✓ (similar) | ✓ |
| `POST /containers/create` | ✓ | ✓ |
| `POST /containers/{id}/start` | ✓ | ✓ |
| `POST /containers/{id}/stop` | ✓ | ✓ |
| `DELETE /containers/{id}` | ✓ | ✓ |
| `GET /containers/{id}/logs` | ✓ | ✓ (streaming) |
| `GET /containers/{id}/stats` | Partial | ✓ (streaming) |
| `GET /images/json` | ✓ | ✓ |
| `POST /images/create` (pull) | ✓ | ✓ |
| `GET /ping` | ✓ | ✓ |
| `POST /exec/create` | ✗ | ✓ |
| `GET /networks` | ✗ | ✓ |
| `GET /volumes` | ✗ | ✓ |
| `GET /events` | ✗ | ✓ (streaming) |

mydocker API không compatible với Docker API format — response schemas khác nhau. Không thể drop-in replace `docker.sock` với `mydocker.sock`.

---

## 5. Networking: mydocker vs Docker

### Docker networking modes

| Mode | mydocker | Docker |
|------|----------|--------|
| bridge (default) | ✓ | ✓ |
| host | ✗ | ✓ |
| none | ✓ (`--no-net`) | ✓ |
| container (share namespace) | ✗ | ✓ |
| overlay (Swarm) | ✗ | ✓ |
| macvlan | ✗ | ✓ |
| ipvlan | ✗ | ✓ |

### IP allocation

Docker dùng IPAM (IP Address Management) plugin với subnet management, gateway config, và conflict detection.

mydocker dùng random IP trong `172.20.0.0/16`:
```go
func allocateIP() string {
    third := rand.Intn(254) + 1
    fourth := rand.Intn(254) + 1
    return fmt.Sprintf("172.20.%d.%d", third, fourth)
}
```

Collision probability: với 254*254 = 64516 possible IPs và birthday paradox, collision probability ~50% sau ~285 containers. Không có collision detection.

### DNS

Docker dùng embedded DNS server (127.0.0.11) trong container — resolve tên container và service names, hỗ trợ round-robin DNS cho load balancing.

mydocker inject vào `/etc/hosts` — đơn giản hơn nhưng không support DNS search domains, không round-robin.

---

## 6. Volumes: mydocker vs Docker

| Feature | mydocker | Docker |
|---------|----------|--------|
| Bind mount | ✓ | ✓ |
| Named volumes | Partial (compose) | ✓ |
| Volume drivers | ✗ | ✓ (NFS, EBS, etc.) |
| `docker volume create` | ✗ | ✓ |
| `docker volume ls` | ✗ | ✓ |
| Volume sharing giữa containers | ✗ | ✓ (`--volumes-from`) |
| tmpfs mount | ✗ | ✓ |
| Read-only bind | ✓ | ✓ |

---

## 7. Dockerfile: mydocker build vs Docker BuildKit

| Feature | mydocker | BuildKit |
|---------|----------|---------|
| FROM | ✓ | ✓ |
| RUN (shell form) | ✓ | ✓ |
| RUN (exec form) | ✓ | ✓ |
| COPY | ✓ | ✓ |
| ADD (local) | ✓ | ✓ |
| ADD (URL) | ✗ | ✓ |
| ENV | ✓ | ✓ |
| WORKDIR | ✓ | ✓ |
| CMD | ✓ | ✓ |
| ENTRYPOINT | ✓ | ✓ |
| EXPOSE | Metadata only | Metadata only |
| ARG | Basic | ✓ |
| LABEL | ✓ | ✓ |
| USER | ✗ (not enforced) | ✓ |
| VOLUME | ✗ | ✓ |
| HEALTHCHECK | ✗ | ✓ |
| ONBUILD | ✗ | ✓ |
| Multi-stage builds | ✗ | ✓ |
| Build cache | ✗ | ✓ |
| Build secrets | ✗ | ✓ |
| Parallel builds | ✗ | ✓ (DAG execution) |
| `.dockerignore` | ✗ | ✓ |

Multi-stage builds là tính năng quan trọng nhất còn thiếu — cần thiết cho build patterns như compile trong build stage, copy binary vào runtime stage.

---

## 8. docker-compose: mydocker-compose vs Compose V2

| Feature | mydocker-compose | Compose V2 |
|---------|-----------------|------------|
| `up` / `down` | ✓ | ✓ |
| `ps` / `logs` | ✓ | ✓ |
| `exec` | ✓ | ✓ |
| `depends_on` (basic) | ✓ | ✓ |
| `depends_on` (condition) | ✗ | ✓ (service_healthy) |
| `healthcheck` | ✗ | ✓ |
| `scale` / `--scale` | ✗ | ✓ |
| Named volumes | Partial | ✓ |
| Network aliases | ✗ | ✓ |
| Multiple networks per service | ✗ | ✓ |
| `profiles` | ✗ | ✓ |
| `secrets` | ✗ | ✓ |
| `configs` | ✗ | ✓ |
| `extends` | ✗ | ✓ |
| Variable substitution | ✗ | ✓ |
| `.env` file | ✗ | ✓ |

`depends_on` với condition (`service_healthy`) quan trọng cho production — đảm bảo service chỉ start sau khi dependency thực sự ready (không chỉ process started).

---

## 9. Kubernetes Integration

### Sử dụng mydocker làm CRI (Container Runtime Interface)

Kubernetes kubelet giao tiếp với container runtime qua CRI (gRPC protocol). containerd và CRI-O implement CRI.

mydocker không implement CRI. Để integrate với Kubernetes cần:

1. Implement CRI gRPC service:
   - `RuntimeService`: RunPodSandbox, CreateContainer, StartContainer, etc.
   - `ImageService`: PullImage, ListImages, etc.

2. Handle pod sandbox concept (shared network namespace cho containers trong cùng pod)

3. Implement CNI (Container Network Interface) thay vì custom bridge

Đây là lượng work đáng kể (~3-4x code hiện tại).

### OCI runtime integration

Kubernetes có thể dùng mydocker như OCI runtime nếu implement `runc`-compatible CLI interface:

```
runc create <id>
runc start <id>
runc state <id>
runc kill <id>
runc delete <id>
```

Hiện tại mydocker không expose CLI interface này.
