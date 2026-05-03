# Kiến trúc mydocker

---

## Mục lục

1. [Tổng quan hệ thống](#1-tổng-quan-hệ-thống)
2. [Cấu trúc thư mục](#2-cấu-trúc-thư-mục)
3. [Luồng chạy lệnh](#3-luồng-chạy-lệnh)
4. [Container lifecycle](#4-container-lifecycle)
5. [Filesystem — Overlayfs](#5-filesystem--overlayfs)
6. [Networking](#6-networking)
7. [Daemon architecture](#7-daemon-architecture)
8. [docker-compose layer](#8-docker-compose-layer)
9. [OCI compatibility](#9-oci-compatibility)
10. [Runtime directory](#10-runtime-directory)
11. [Dependency graph](#11-dependency-graph)

---

## 1. Tổng quan hệ thống

```
┌────────────────────────────────────────────────────┐
│                          User                      │
└───────────┬─────────────────────┬──────────────────┘
            │                     │
            ▼                     ▼
   ┌─────────────────┐   ┌──────────────────────┐
   │   mydocker      │   │  mydocker-compose    │
   │   (CLI)         │   │  (compose CLI)       │
   └────────┬────────┘   └──────────┬───────────┘
            │                       │
            │  Unix socket          │  gọi trực tiếp
            ▼                       │
   ┌─────────────────┐              │
   │   mydockerd     │◄─────────────┘
   │   (daemon)      │
   └────────┬────────┘
            │
            ▼
   ┌─────────────────────────────────────────────┐
   │              internal packages              │
   │  container │ image │ network │ cgroup │ ... │
   └─────────────────────────────────────────────┘
            │
            ▼
   ┌─────────────────────────────────────────────┐
   │              Linux Kernel                   │
   │  namespaces │ cgroups v2 │ overlayfs        │
   └─────────────────────────────────────────────┘
```

**3 binary chính:**
- `mydocker` — CLI, giao tiếp với daemon qua Unix socket. Nếu không có daemon, tự chạy direct mode.
- `mydockerd` — Daemon chạy background, lắng nghe `/var/run/mydocker.sock`, quản lý toàn bộ container lifecycle.
- `mydocker-compose` — CLI riêng cho compose, parse `docker-compose.yml` và gọi vào `internal/container`.

---

## 2. Cấu trúc thư mục

```
mydocker/
├── cmd/
│   ├── mydocker/           ← CLI entry point
│   ├── mydockerd/          ← Daemon entry point
│   └── mydocker-compose/   ← Compose CLI entry point
│
├── internal/
│   ├── api/                ← HTTP server/client (daemon ↔ CLI)
│   ├── container/          ← Core: namespace, rootfs, lifecycle
│   ├── image/              ← Pull từ Docker Hub, lưu layers
│   ├── build/              ← Dockerfile parser + builder
│   ├── compose/            ← docker-compose.yml parser + runner
│   ├── cgroup/             ← cgroup v2 resource limits
│   ├── network/            ← bridge, veth, NAT, DNS, port forward
│   ├── overlay/            ← overlayfs mount/unmount
│   ├── oci/                ← OCI Runtime Spec (config.json)
│   ├── state/              ← container state persistence
│   └── capability/         ← Linux capabilities (placeholder)
│
├── scripts/
│   ├── setup.sh            ← system setup (cgroup, bridge, dirs)
│   ├── quick-start.sh      ← build + install + setup 1 lệnh
│   └── setup-rootless.sh   ← rootless container setup
│
├── example/
│   └── docker-compose.yml  ← compose example
│
├── test/
│   └── integration/e2e.sh  ← E2E test script
│
├── Makefile
├── go.mod
└── go.sum
```

---

## 3. Luồng chạy lệnh

### 3.1 Với daemon (`mydocker run -d`)

```
User
 │
 ▼
mydocker (CLI)
 │  kiểm tra /var/run/mydocker.sock
 │  daemon available + detach mode
 │
 ├─── POST /containers ──────────────► mydockerd
 │                                        │ CreateContainer()
 │                                        │ tạo containerDir
 │                                        │ lưu state.json
 │                                        │ trả về containerID
 │◄── {id: "abc123"} ─────────────────────┘
 │
 ├─── POST /containers/abc123/start ──► mydockerd
 │                                        │ StartContainer()
 │                                        │ resolve image config
 │                                        │ setup cgroup
 │                                        │ fork: exec /proc/self/exe init
 │                                        │ setup network (veth)
 │                                        │ update state → running
 │◄── 204 No Content ─────────────────────┘
 │
 ▼
print containerID → trả quyền terminal cho user
```

### 3.2 Direct mode (`mydocker run -it`)

```
User
 │
 ▼
mydocker (CLI)
 │  không có daemon / interactive mode
 │
 ▼
container.Run(cfg)
 │
 ├── generateID()
 ├── cgroup.Create() + SetLimits()
 ├── network.SetupBridge() + veth.Create()
 ├── exec: /proc/self/exe init <id> <image> <cmd>
 │     └── SysProcAttr.Cloneflags = NEWUTS|NEWPID|NEWNS|NEWIPC|NEWNET
 │
 ▼
container.Init()  [chạy trong namespace mới — PID 1]
 │
 ├── MS_PRIVATE|MS_REC  (private mount propagation)
 ├── mount overlayfs → mergedDir
 ├── bind mount volumes → mergedDir
 ├── pivot_root(mergedDir)
 ├── mount /proc /sys /dev /tmp /run
 ├── Sethostname()
 └── syscall.Exec(command)  ← replace process, không return
```

**Tại sao dùng reexec pattern?**
- `SysProcAttr.Cloneflags` chỉ hoạt động khi fork child process
- Child cần chạy trong namespace mới trước khi setup rootfs
- Giải pháp: child exec lại chính binary với argument `init`

---

## 4. Container lifecycle

```
         pull image
              │
              ▼
         ┌─────────┐
         │ created │  ← generateID, tạo dirs, lưu state
         └────┬────┘
              │ start
              ▼
         ┌─────────┐
         │ running │  ← process đang chạy, cgroup active
         └────┬────┘
              │
       ┌──────┴──────┐
       │             │
   exit/crash    mydocker stop
       │             │ SIGTERM → wait 10s → SIGKILL
       ▼             ▼
    ┌────────────────────┐
    │       exited       │  ← exit code lưu vào state.json
    └──────────┬─────────┘
               │
       restart policy?
       ┌───────┴───────┐
      yes              no
       │               │
       ▼               ▼
   [restart]      mydocker rm
                       │
                       ▼
                  ┌──────────┐
                  │ deleted  │  ← xóa containerDir, unmount overlayfs
                  └──────────┘
```

**State được lưu tại:** `/var/lib/mydocker/containers/<id>/state.json`

```json
{
  "id": "abc123",
  "image": "alpine:latest",
  "command": ["/bin/sh"],
  "pid": 12345,
  "status": "running",
  "ip": "172.20.15.23",
  "ports": [{"host_port": 8080, "container_port": 80}],
  "created_at": "2026-05-03T15:00:00Z"
}
```

---

## 5. Filesystem — Overlayfs

### 5.1 Image layers

```
Docker Hub
    │  pull
    ▼
Layer 1 (sha256:3531af...)  ←── base OS (debian, alpine...)
Layer 2 (sha256:ce776b...)  ←── apt install / apk add
Layer 3 (sha256:85c661...)  ←── config files
    ...
Layer N (sha256:801a1a...)  ←── entrypoint, cmd

Lưu tại: /var/lib/mydocker/images/<name>_<tag>/layers/<sha256>/
```

### 5.2 Overlayfs khi run container

```
┌─────────────────────────────────────┐
│              container thấy         │
│         /  (merged view)            │
└──────────────────┬──────────────────┘
                   │  overlayfs
        ┌──────────┴──────────┐
        │                     │
        ▼                     ▼
  ┌───────────┐         ┌───────────┐
  │  upperdir │  (R/W)  │  lowerdir │  (R/O)
  │ container │         │  image    │
  │ writes    │         │  layers   │
  └───────────┘         └───────────┘
  /containers/<id>/upper/   /images/<name>/layers/
```

**Nguyên tắc:**
- Container ghi → vào `upper/` (không ảnh hưởng image gốc)
- Container đọc file không có trong upper → đọc từ `lower/` (image)
- Nhiều containers dùng cùng image → dùng chung `lower/`, `upper/` riêng biệt
- Khi `rm` container → xóa `upper/`, image không bị ảnh hưởng

### 5.3 Pivot root

```
Trước pivot_root:           Sau pivot_root:
/  (WSL2 filesystem)        /  (Alpine rootfs)
├── home/                   ├── bin/
├── var/                    ├── etc/
│   └── lib/mydocker/       ├── lib/
│       └── containers/     ├── proc/   ← mount mới
│           └── <id>/       ├── sys/    ← mount mới
│               └── merged/ ├── dev/    ← mount mới
│                   └── ...  └── ...
```

`pivot_root` thay thế `/` của process bằng `mergedDir` — không phải `chroot` (kém bảo mật hơn).

---

## 6. Networking

### 6.1 Kiến trúc mạng

```
Host (WSL2)
┌────────────────────────────────────────────────────────┐
│                                                        │
│  eth0 (172.x.x.x)  ←── Hyper-V virtual switch          │
│       │                                                │
│       │  iptables MASQUERADE                           │
│       │                                                │
│  mydocker0 bridge (172.20.0.1/16)                      │
│       │                                                │
│  ┌────┴────┐    ┌──────────┐    ┌──────────┐           │
│  │ veth-h-1│    │ veth-h-2 │    │ veth-h-3 │  (host)   │
│  └────┬────┘    └────┬─────┘    └────┬─────┘           │
└───────┼──────────────┼───────────────┼─────────────────┘
        │ network      │ namespace     │
        ▼              ▼               ▼
  ┌──────────┐  ┌──────────┐  ┌──────────┐
  │container1│  │container2│  │container3│
  │veth-c-1  │  │veth-c-2  │  │veth-c-3  │
  │172.20.x.x│  │172.20.x.x│  │172.20.x.x│
  └──────────┘  └──────────┘  └──────────┘
```

### 6.2 Tạo network cho mỗi container

```
1. ip link add veth-h-<id> type veth peer name veth-c-<id>
2. ip link set veth-h-<id> master mydocker0
3. ip link set veth-h-<id> up
4. ip link set veth-c-<id> netns <container_pid>
5. (trong container namespace):
   ip link set veth-c-<id> up
   ip addr add 172.20.x.x/16 dev veth-c-<id>
   ip route add default via 172.20.0.1
```

### 6.3 Port forwarding

```
Request từ ngoài vào port 8080
          │
          ▼
    iptables PREROUTING
    DNAT: :8080 → 172.20.15.23:80
          │
          ▼
    mydocker0 bridge
          │
          ▼
    container (172.20.15.23:80)
```

### 6.4 Container DNS

```
Container A (--name web)          Container B
IP: 172.20.15.23                  IP: 172.20.20.1
                                       │
                                  wget http://web
                                       │
                                  /etc/hosts lookup
                                       │
                         ┌─────────────┘
                         ▼
              /var/lib/mydocker/dns/hosts
              172.20.15.23  web  #containerID
                         │
                         ▼
              resolve → 172.20.15.23
```

DNS entries được inject vào `/etc/hosts` của mỗi container khi start.

---

## 7. Daemon architecture

### 7.1 Daemon components

```
┌───────────────────────────────────────────────┐
│                    mydockerd                  │
│                                               │
│  ┌─────────────────────────────────────────┐  │
│  │           HTTP Server (api/server.go)   │  │
│  │   /containers  /images  /build  /ping   │  │
│  └──────────────────┬──────────────────────┘  │
│                     │                         │
│  ┌──────────────────▼─────────────────────┐   │
│  │           Daemon handler               │   │
│  │  CreateContainer  StartContainer       │   │
│  │  StopContainer    RemoveContainer      │   │
│  │  ListContainers   GetLogs / GetStats   │   │
│  └──────────────────┬─────────────────────┘   │
│                     │                         │
│  ┌──────────────────▼────────────────────┐    │
│  │         internal packages             │    │
│  │  container │ image │ network │ cgroup │    │
│  └───────────────────────────────────────┘    │
│                                               │
│  /var/run/mydocker.sock  (Unix socket)        │
└───────────────────────────────────────────────┘
```

### 7.2 CLI ↔ Daemon communication

```
mydocker run -d nginx
     │
     ▼
api/client.go
  POST http://daemon/containers
  body: CreateContainerRequest{Image: "nginx", ...}
     │
     │  Unix socket: /var/run/mydocker.sock
     ▼
api/server.go
  handleContainers() → POST
     │
     ▼
daemon.CreateContainer()
     │
     ▼
  return {id: "abc123"}
     │
     ▼
api/client.go
  POST http://daemon/containers/abc123/start
     │
     ▼
daemon.StartContainer()
  → fork container process
  → watchContainer() goroutine (handle restart policy)
```

### 7.3 Restart policy — goroutine watcher

```
daemon.StartContainer()
    │
    ├── cmd.Start()  ← fork container
    │
    └── go watchContainer()  ← goroutine chạy song song
              │
              │ cmd.Wait()  ← block cho đến khi container exit
              │
              ▼
        đọc restart_policy file
              │
        ┌─────┴──────┐
       "always"   "on-failure"   "unless-stopped"   ""
        restart    exit!=0?        !manually_stopped  không làm gì
                   │ yes
                   restart
```

---

## 8. docker-compose layer

```
docker-compose.yml
       │
       ▼
compose/parser.go
  Parse() → ComposeFile struct
  Resolve() → []ResolvedService
       │
       │  topoSort() theo depends_on
       ▼
compose/runner.go
  Runner.Up()
       │
       ├── service 1 (không có depends_on)
       │       │
       │       ├── imageExists? → pull nếu chưa có
       │       ├── ShouldBuild? → build.Build()
       │       └── container.Run(cfg)  ← direct mode
       │
       ├── service 2 (depends_on: service1)
       │       └── container.Run(cfg)
       │
       └── service N
               └── container.Run(cfg)
```

**Thứ tự start:** topological sort theo `depends_on`

```
Ví dụ depends_on graph:
  frontend → (không có)
  api      → frontend
  db       → (không có)

Thứ tự start: db, frontend, api
```

---

## 9. OCI compatibility

### 9.1 OCI Runtime Spec

```
container.Run()
    │
    ├── ... (tạo container)
    │
    └── buildOCISpec() → config.json
            │
            ▼
    /var/lib/mydocker/containers/<id>/bundle/config.json

config.json structure (OCI v1.0.2):
    {
      "ociVersion": "1.0.2",
      "process": { args, env, cwd, capabilities },
      "root":    { path: merged/ },
      "mounts":  [ /proc, /sys, /dev, volumes... ],
      "linux":   { namespaces, cgroupsPath, resources }
    }
```

### 9.2 Tương thích với runc

```
mydocker image layers
        │
        ▼
/var/lib/mydocker/images/<name>/layers/<sha>/
        │
        │  copy rootfs
        ▼
OCI bundle:
  /tmp/bundle/
  ├── config.json   ← OCI spec
  └── rootfs/       ← Alpine/nginx filesystem
        │
        ▼
  runc run --bundle /tmp/bundle <id>
        │
        ▼
  Container chạy bằng runc production runtime
```

---

## 10. Runtime directory

```
/var/lib/mydocker/
│
├── images/
│   ├── alpine_latest/
│   │   ├── manifest.json
│   │   ├── config.json       ← Entrypoint, Cmd, Env, WorkingDir
│   │   └── layers/
│   │       └── 6a0ac1617861/ ← extracted layer (Alpine rootfs)
│   │
│   └── nginx_latest/
│       ├── manifest.json
│       ├── config.json
│       └── layers/
│           ├── 3531af2bc2a9/ ← layer 1 (base debian)
│           ├── ce776bbcda0d/ ← layer 2 (apt packages)
│           └── ...           ← layer 3-7
│
├── containers/
│   └── abc123def-456/
│       ├── state.json        ← PID, status, IP, ports
│       ├── config.json       ← volumes, env, hostname
│       ├── container.log     ← stdout/stderr (detach mode)
│       ├── restart_policy    ← "always" / "on-failure" / "no"
│       ├── upper/            ← overlayfs upper (container writes)
│       ├── work/             ← overlayfs work dir
│       ├── merged/           ← overlayfs mount point (rootfs)
│       └── bundle/
│           └── config.json   ← OCI Runtime Spec
│
├── dns/
│   └── hosts                 ← container name → IP mappings
│
└── volumes/
    └── <named_volume>/       ← named volumes từ compose
```

---

## 11. Dependency graph

### 11.1 Internal package dependencies

```
cmd/mydocker
    ├── internal/api (client)
    ├── internal/container
    ├── internal/build
    └── internal/state (qua api)

cmd/mydockerd
    ├── internal/api (server)
    ├── internal/container
    ├── internal/image
    ├── internal/network
    ├── internal/cgroup
    └── internal/state

cmd/mydocker-compose
    ├── internal/compose
    │       ├── internal/container
    │       ├── internal/image
    │       └── internal/build
    └── internal/state

internal/container
    ├── internal/cgroup
    ├── internal/network
    ├── internal/state
    ├── internal/image  (LoadImageConfig)
    └── internal/oci    (GenerateSpec)

internal/network
    ├── bridge.go       (mydocker0, iptables)
    ├── veth.go         (veth pair per container)
    ├── portforward.go  (iptables DNAT)
    └── dns.go          (hosts file)
```

### 11.2 External dependencies

```
go.mod:
  github.com/google/uuid v1.6.0    ← container ID generation
  gopkg.in/yaml.v3 v3.0.1          ← docker-compose.yml parsing

System tools (runtime):
  ip / iproute2    ← network setup
  iptables         ← NAT, port forwarding
  nsenter          ← exec vào container namespace
  runc (optional)  ← OCI runtime thay thế
  unshare          ← build isolation (mydocker build)
```

### 11.3 Kernel features được dùng

| Kernel Feature | Dùng cho | Syscall / Interface |
|---------------|----------|---------------------|
| PID namespace | Process isolation | `CLONE_NEWPID` |
| Mount namespace | Filesystem isolation | `CLONE_NEWNS` |
| UTS namespace | Hostname isolation | `CLONE_NEWUTS` |
| IPC namespace | IPC isolation | `CLONE_NEWIPC` |
| Network namespace | Network isolation | `CLONE_NEWNET` |
| User namespace | Rootless containers | `CLONE_NEWUSER` |
| cgroup v2 | Resource limits | `/sys/fs/cgroup/` |
| overlayfs | Union filesystem | `mount("overlay", ...)` |
| pivot_root | Root filesystem swap | `syscall.PivotRoot()` |
| seccomp | Syscall filtering | (placeholder) |
