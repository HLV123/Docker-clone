# Build Roadmap

Ghi lại quá trình xây dựng mydocker theo từng phase. Mỗi phase là một milestone độc lập, có thể verify bằng lệnh cụ thể. Không ghi lại quá trình debug — chỉ ghi trạng thái cuối khi phase hoàn thành.

**Môi trường:** WSL2 Ubuntu 22.04, Go 1.23, kernel 6.6.87.2-microsoft-standard-WSL2

---

## Phase 1 — Namespace Isolation

**Mục tiêu:** Container là một process chạy trong namespace riêng. Chưa có filesystem isolation, chưa có image, chưa có network.

**Những gì đã build:**
- `cmd/mydocker/main.go` — CLI dispatch với `run` và `init` commands
- `internal/container/container.go` — `Run()`: fork với `CLONE_NEWUTS|CLONE_NEWPID|CLONE_NEWNS|CLONE_NEWIPC`
- `internal/container/init.go` — `Init()`: `Sethostname`, mount `/proc`, `syscall.Exec`
- Reexec pattern: child exec `/proc/self/exe init` để chạy trong namespace mới

**Verify:**

```bash
go build -o mydocker ./cmd/mydocker
sudo ./mydocker run /bin/bash
```

Trong container:
```bash
hostname        # container (không phải hostname của WSL2)
echo $$         # 1 (PID 1 trong PID namespace mới)
ps aux          # chỉ thấy bash và ps, không thấy host processes
exit
```

**Hoàn thành khi:** Hostname khác host, PID 1 là bash, `ps aux` chỉ thấy container processes.

---

## Phase 2 — Filesystem Isolation với Overlayfs

**Mục tiêu:** Container có filesystem riêng từ Alpine image. Thay đổi trong container không ảnh hưởng image gốc.

**Những gì đã build:**
- `internal/container/rootfs.go` — `SetupRootfs()`: mount overlayfs, `pivotRoot()`
- `internal/overlay/overlay.go` — `Mount()`, `Unmount()`
- Update `init.go` — gọi `SetupRootfs()` trước `Sethostname`
- Alpine base image setup thủ công tại `/var/lib/mydocker/images/alpine_latest/layers/`

**Verify:**

```bash
# Setup Alpine image thủ công
sudo mkdir -p /var/lib/mydocker/images/alpine_latest/layers/base
curl -L https://dl-cdn.alpinelinux.org/alpine/v3.18/releases/x86_64/alpine-minirootfs-3.18.0-x86_64.tar.gz \
  | sudo tar -xz -C /var/lib/mydocker/images/alpine_latest/layers/base

sudo ./mydocker run alpine:latest /bin/sh
```

Trong container:
```bash
cat /etc/os-release   # NAME="Alpine Linux"
ls /                  # Alpine filesystem, không thấy WSL2 directories
echo "test" > /tmp/hello.txt
exit
```

Sau khi exit:
```bash
# File trong container không ảnh hưởng image
ls /var/lib/mydocker/images/alpine_latest/layers/base/tmp/
# không có hello.txt

# File nằm trong upper layer của container (đã exit, container dir bị xóa hoặc còn đó)
```

**Hoàn thành khi:** `cat /etc/os-release` hiện Alpine, filesystem hoàn toàn khác host.

---

## Phase 3 — cgroup v2 Resource Limits

**Mục tiêu:** Container bị giới hạn memory, CPU, và số processes.

**Những gì đã build:**
- `internal/cgroup/cgroup.go` — `Create()`, `SetLimits()`, `AddProcess()`, `Destroy()`
- Update `container.go` — tạo cgroup trước fork, add PID sau fork

**Verify:**

```bash
# Enable cgroup controllers (một lần)
sudo bash -c 'echo "+memory +cpu +pids" > /sys/fs/cgroup/cgroup.subtree_control'
sudo mkdir -p /sys/fs/cgroup/mydocker
sudo bash -c 'echo "+memory +cpu +pids" > /sys/fs/cgroup/mydocker/cgroup.subtree_control'

# Chạy container với memory limit
sudo ./mydocker run -d --memory=50m alpine:latest /bin/sh -c "sleep 60"
ID=$(sudo ./mydocker ps | tail -1 | awk '{print $1}')

# Verify memory limit được set
cat /sys/fs/cgroup/mydocker/$ID/memory.max
# 52428800 (50MB)
```

**Hoàn thành khi:** `memory.max` đọc ra đúng giá trị đã set.

---

## Phase 4 — Pull Image từ Docker Hub

**Mục tiêu:** `mydocker pull` tải image từ Docker Hub Registry API v2, verify SHA256, extract layers.

**Những gì đã build:**
- `internal/image/pull.go` — `Pull()`, `getToken()`, `getManifest()`, `downloadLayer()`, `extractTarGz()`
- `internal/image/list.go` — `List()`
- Update `main.go` — thêm `pull` và `images` commands
- Fix `findImageBase()` trong `rootfs.go` — hỗ trợ multi-layer từ pull
- Fix lowerdir naming: `alpine:latest` → `alpine_latest` (colon breaks overlayfs lowerdir syntax)
- Fix manifest list: detect multi-arch manifest, resolve `linux/amd64`

**Verify:**

```bash
sudo ./mydocker pull alpine:latest
# Pulling alpine:latest from Docker Hub...
# Fetching manifest...
# Pulling 1 layers...
# Successfully pulled alpine:latest

sudo ./mydocker images
# REPOSITORY   TAG      LAYERS   SIZE
# alpine       latest   1        8.1 MB

sudo ./mydocker run alpine:latest /bin/sh -c "cat /etc/alpine-release"
# 3.23.4  (phiên bản mới nhất từ Docker Hub)
```

**Hoàn thành khi:** Pull thành công, `images` list hiện đúng, container chạy từ image pulled.

---

## Phase 5 — State Management

**Mục tiêu:** Track container lifecycle. `ps`, `rm`, `stop` hoạt động đúng.

**Những gì đã build:**
- `internal/state/state.go` — `Save()`, `Load()`, `PS()`, `Remove()`, `Logs()`
- Container state: `created → running → exited`
- `syncStatus()` — check PID còn sống bằng `kill(pid, 0)`
- Update `container.go` — save state tại mỗi lifecycle transition

**Verify:**

```bash
# Chạy vài containers
sudo ./mydocker run -d alpine:latest /bin/sh -c "sleep 30"
sudo ./mydocker run -d alpine:latest /bin/sh -c "echo hello"
sudo ./mydocker run -it alpine:latest  # exit ngay

sudo ./mydocker ps -a
# CONTAINER ID   IMAGE           COMMAND   STATUS    CREATED
# abc123...      alpine:latest   /bin/sh   running   2026-05-03 ...
# def456...      alpine:latest   /bin/sh   exited    2026-05-03 ...
# ghi789...      alpine:latest   /bin/sh   exited    2026-05-03 ...

# Remove exited container
sudo ./mydocker rm def456
ls /var/lib/mydocker/containers/def456  # No such file
```

**Hoàn thành khi:** `ps -a` hiện đúng status, `rm` xóa container dir, `rm` container đang running báo lỗi.

---

## Phase 6 — Networking

**Mục tiêu:** Container có IP riêng, access được internet, containers gọi nhau bằng tên.

**Những gì đã build:**
- `internal/network/bridge.go` — `SetupBridge()`: tạo `mydocker0`, iptables MASQUERADE
- `internal/network/veth.go` — `Create()`, `MoveToNetns()`, `SetupInsideContainer()`
- `internal/network/portforward.go` — `AddPortForward()`, `RemovePortForward()` (iptables DNAT)
- `internal/network/dns.go` — `RegisterContainer()`, `UnregisterContainer()`, `InjectDNS()`
- Update `container.go` — `--net` flag, `CLONE_NEWNET`, setup veth sau fork
- Update `init.go` — `network.InjectDNS(mergedDir)` trước pivot_root

**Verify:**

```bash
# Test internet access
sudo ./mydocker run --net alpine:latest /bin/sh -c "wget -q -O- http://example.com | head -3"
# <!doctype html>...

# Test port forwarding
sudo ./mydocker run -d --net -p 8080:80 --name web nginx:latest
sleep 2
IP=$(sudo ./mydocker ps | grep web | awk '{print $7}')
curl http://$IP
# <!DOCTYPE html>...

# Test container DNS
sudo ./mydocker run -d --net --name api alpine:latest /bin/sh -c "sleep 60"
sudo ./mydocker run --net alpine:latest /bin/sh -c "wget -q -O- http://web | head -3"
# <!DOCTYPE html>...  (gọi nginx bằng tên "web")
```

**Hoàn thành khi:** Container access internet, port forwarding hoạt động, container gọi nhau bằng tên.

---

## Phase 7 — Exec và Logs

**Mục tiêu:** `exec` vào container đang chạy, `logs` xem output của detached container.

**Những gì đã build:**
- `internal/container/exec.go` — `Exec()` dùng `nsenter` với `--target=<pid> --mount --uts --ipc --pid`
- Update `state.go` — `Logs()` đọc `container.log`
- Update `container.go` — detach mode redirect stdout/stderr vào `container.log`

**Verify:**

```bash
# Detach container
sudo ./mydocker run -d alpine:latest /bin/sh -c "while true; do date; sleep 2; done"
ID=$(sudo ./mydocker ps | tail -1 | awk '{print $1}')

# Xem logs
sudo ./mydocker logs $ID
# Sun May  3 14:00:00 UTC 2026
# Sun May  3 14:00:02 UTC 2026

# Exec vào container đang chạy
sudo ./mydocker exec $ID /bin/sh
# / # hostname    → container-abc123
# / # ps aux      → thấy process của container
# / # exit
```

**Hoàn thành khi:** `logs` hiện output, `exec` mở shell trong đúng namespace của container.

---

## Phase 8 — Batch 1 Features

**Mục tiêu:** Port forwarding, volume mount, env vars, image config auto-detect, `stop`, `inspect`, `stats`.

**Những gì đã build:**
- `container.Config` mở rộng: `Ports`, `Volumes`, `Env`, `TTY`, `Interactive`, `Name`, `Hostname`, `WorkingDir`, `Entrypoint`
- Volume mount trước pivot_root: bind mount vào `mergedDir` khi host path còn accessible
- Env vars: truyền qua `cmd.Env` thay vì inherit từ host
- Image config resolution trong `resolveCommand()`: đọc `config.json` → tự detect Entrypoint/Cmd
- `container.Stop()`: SIGTERM → wait 10s → SIGKILL
- `state.Stats()`: đọc từ `/sys/fs/cgroup/mydocker/<id>/`
- `image.LoadImageConfig()`: đọc `config.json` của image
- Pull cập nhật: download config blob để lưu Entrypoint/Cmd/Env/WorkingDir

**Verify:**

```bash
# Env vars
sudo ./mydocker run -e MYVAR=hello alpine:latest /bin/sh -c "echo $MYVAR"
# hello

# Volume mount
mkdir -p /tmp/testdata && echo "from host" > /tmp/testdata/test.txt
sudo ./mydocker run -v /tmp/testdata:/data alpine:latest /bin/sh -c "cat /data/test.txt"
# from host

# Image config auto-detect
sudo ./mydocker pull nginx:latest
sudo ./mydocker run -d --net --name web nginx:latest  # không cần gõ entrypoint thủ công
sleep 2
sudo ./mydocker ps | grep web  # status: running, command: /docker-entrypoint.sh

# Stop graceful
sudo ./mydocker stop web
# Stopping container...
# Container stopped

# Inspect
sudo ./mydocker inspect nginx:latest
# {"Entrypoint": ["/docker-entrypoint.sh"], "Cmd": ["nginx", "-g", "daemon off;"], ...}
```

**Hoàn thành khi:** Tất cả commands trên chạy đúng.

---

## Phase 9 — Dockerfile Build

**Mục tiêu:** `mydocker build` parse Dockerfile, execute từng instruction, tạo image mới.

**Những gì đã build:**
- `internal/build/dockerfile.go` — parser: `FROM`, `RUN`, `COPY`, `ENV`, `WORKDIR`, `CMD`, `ENTRYPOINT`, `EXPOSE`, `ARG`, `LABEL`
- `internal/build/builder.go` — `Build()`: execute instructions, tạo layers per RUN, copy files per COPY
- Update `rootfs.go` — `buildLowerDirWithBase()`: chain build layers + base image layers
- Fix: `base_layers` file để reference base image path thay vì copy

**Verify:**

```bash
mkdir -p /tmp/myapp
printf '#!/bin/sh\necho "App running! ENV=$APP_ENV"\n' > /tmp/myapp/app.sh

cat > /tmp/myapp/Dockerfile << 'EOF'
FROM alpine:latest
RUN echo "hello from build" > /hello.txt
COPY app.sh /app.sh
ENV APP_ENV=production
WORKDIR /app
CMD ["/bin/sh", "/app.sh"]
EOF

sudo ./mydocker build -t myapp:v1 /tmp/myapp
# Step 1/6 : FROM alpine:latest
# Step 2/6 : RUN echo "hello from build" > /hello.txt
# Step 3/6 : COPY app.sh /app.sh
# ...
# Successfully built myapp:v1

sudo ./mydocker run myapp:v1
# App running! ENV=production

sudo ./mydocker run myapp:v1 /bin/sh -c "cat /hello.txt"
# hello from build
```

**Hoàn thành khi:** Build thành công, CMD tự chạy, RUN và COPY có hiệu lực.

---

## Phase 10 — Daemon Architecture

**Mục tiêu:** `mydockerd` chạy background, CLI giao tiếp qua Unix socket, restart policy hoạt động.

**Những gì đã build:**
- `internal/api/types.go` — request/response types
- `internal/api/server.go` — HTTP server trên Unix socket `/var/run/mydocker.sock`
- `internal/api/client.go` — HTTP client
- `cmd/mydockerd/main.go` — daemon: implement `ContainerHandler`, `watchContainer()` goroutine
- Update `cmd/mydocker/main.go` — check daemon availability, route qua daemon hoặc direct mode

**Verify:**

```bash
# Terminal 1: start daemon
sudo mydockerd &

# Terminal 2
sleep 1

# Daemon available
ls /var/run/mydocker.sock  # socket tồn tại

# Test qua daemon
mydocker run -d --name web nginx:latest
sleep 2
mydocker ps
# web | nginx:latest | running

mydocker logs web | head -3
# nginx startup logs

# Restart policy
mydocker run -d --restart=always --name auto alpine:latest /bin/sh -c "sleep 3"
sleep 8
mydocker ps | grep auto  # vẫn running (đã restart)

mydocker stop web auto
```

**Hoàn thành khi:** Daemon chạy background, CLI route qua socket, restart policy trigger.

---

## Phase 11 — docker-compose

**Mục tiêu:** `mydocker-compose up/down/ps/logs/exec` với `docker-compose.yml`.

**Những gì đã build:**
- `internal/compose/types.go` — ComposeFile, Service, ResolvedService structs
- `internal/compose/parser.go` — YAML parser, `Resolve()`, `topoSort()` theo `depends_on`
- `internal/compose/runner.go` — `Up()`, `Down()`, `PS()`, `Logs()`, `ExecService()`
- `cmd/mydocker-compose/main.go` — CLI entry point

**Verify:**

```bash
cd ~/projects/mydocker/example
cat docker-compose.yml  # web + api + db services

sudo mydocker-compose up -d
# [example] Starting services...
#   [web] Starting container... ✓
#   [api] Starting container... ✓
#   [db] Starting container... ✓

sudo mydocker-compose ps
# example_web | nginx:latest | running | 8080->80

# Container gọi nhau bằng tên service
WEB_IP=$(mydocker ps | grep example_web | awk '{print $7}')
curl http://$WEB_IP  # <h1>Hello from mydocker-compose!</h1>

sudo mydocker-compose logs web | tail -5
sudo mydocker-compose exec api /bin/sh -c "wget -q -O- http://web | head -3"
# <!DOCTYPE html>...

sudo mydocker-compose down
# [example] All services stopped.
```

**Hoàn thành khi:** 3 services up, web serve đúng nội dung, containers gọi nhau bằng tên.

---

## Phase 12 — OCI Compatibility

**Mục tiêu:** Mỗi container có `config.json` theo OCI Runtime Spec v1.0.2. Rootfs compatible với `runc`.

**Những gì đã build:**
- `internal/oci/spec.go` — OCI Runtime Spec types
- `internal/oci/generate.go` — `GenerateSpec()`, `SaveSpec()`, `ValidateSpec()`
- `internal/oci/runtime.go` — `Runtime` wrapper, detect `runc`/`crun`
- Update `container.go` — `buildOCISpec()`, save `bundle/config.json` per container
- Update `main.go` — `mydocker spec <id>`, `mydocker runtime`

**Verify:**

```bash
# Xem runtime
mydocker runtime
# Runtime: runc
# Available: true

# Chạy container, xem OCI spec
sudo rm -f /var/run/mydocker.sock  # đảm bảo direct mode
ID=$(mydocker run -d alpine:latest /bin/sh -c "sleep 30")
sleep 1

mydocker spec $ID
# {"ociVersion": "1.0.2", "hostname": "container-...", "process": {...}, "linux": {...}}

cat /var/lib/mydocker/containers/$ID/bundle/config.json | python3 -m json.tool | head -10

# Chạy Alpine bằng runc trực tiếp
sudo mkdir -p /tmp/runc-test/rootfs
sudo cp -r /var/lib/mydocker/images/alpine_latest/layers/*/. /tmp/runc-test/rootfs/
sudo runc spec --bundle /tmp/runc-test
sudo python3 -c "
import json
with open('/tmp/runc-test/config.json') as f: c = json.load(f)
c['process']['args'] = ['/bin/sh', '-c', 'echo Hello from runc!']
c['process']['terminal'] = False
open('/tmp/runc-test/config.json', 'w').write(json.dumps(c))
"
sudo runc run --bundle /tmp/runc-test test-oci
# Hello from runc!
```

**Hoàn thành khi:** `mydocker spec` output valid JSON theo OCI spec, runc chạy được rootfs từ mydocker image.

---

## Tổng kết

| Phase | Core feature | Kernel primitive |
|-------|-------------|-----------------|
| 1 | Namespace isolation | `clone()` flags, `unshare()` |
| 2 | Filesystem isolation | overlayfs, `pivot_root()` |
| 3 | Resource limits | cgroup v2 |
| 4 | Image management | Docker Registry API v2 |
| 5 | State management | — |
| 6 | Networking | veth, bridge, iptables, network namespace |
| 7 | Exec + Logs | `setns()` via nsenter |
| 8 | UX features | — |
| 9 | Image build | overlayfs multi-layer, `unshare` |
| 10 | Daemon | Unix socket, goroutines |
| 11 | Compose | YAML parsing, dependency ordering |
| 12 | OCI | OCI Runtime Spec |
