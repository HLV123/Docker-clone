# Security Analysis

Phân tích bề mặt tấn công, threat model, và so sánh security posture của mydocker với production container runtimes.

---

## 1. Threat Model

### Trust boundary

```
┌────────────────────────────────────────────────────┐
│  Trusted                                           │
│  ┌──────────────────────────────────────────────┐  │
│  │  mydocker CLI / mydockerd (chạy với root)    │  │
│  └──────────────────────────────────────────────┘  │
│                     │                              │
│             trust boundary                         │
│                     │                              │
│  ┌──────────────────▼───────────────────────────┐  │
│  │  Container process (untrusted user code)     │  │
│  └──────────────────────────────────────────────┘  │
└────────────────────────────────────────────────────┘
```

### Attacker assumptions

- Container process có thể là malicious code
- Attacker có full control bên trong container
- Attacker biết đang chạy trong container (không phải VM)
- Attacker có thể đọc `/proc/self/status`, `/proc/self/cgroup`, etc.

### Security goals

- **Container escape**: attacker không thể access host filesystem, process, network ngoài những gì được cấp
- **Resource exhaustion**: container không thể exhaust CPU/RAM của host (cgroup limits)
- **Privilege escalation**: container root không tương đương host root

---

## 2. Namespace isolation — gì được bảo vệ, gì không

### Được bảo vệ

| Resource | Namespace | Mức độ |
|----------|-----------|--------|
| Process list | PID | Hoàn toàn — container không thấy host processes |
| Hostname | UTS | Hoàn toàn |
| Network stack | NET | Hoàn toàn nếu `--net` — riêng interface, routing, iptables |
| Filesystem | Mount + pivot_root | Hoàn toàn — không access host paths |
| IPC | IPC | Hoàn toàn — shared memory, semaphores riêng |

### Không được bảo vệ trong mydocker

**`/proc/sys/` kernel parameters**: container có thể đọc và một số có thể ghi kernel parameters ảnh hưởng toàn system nếu không bị chặn.

**Time**: không có time namespace — container thấy cùng system time với host. Thay đổi time (nếu có capability) ảnh hưởng host.

**Kernel keyring**: không bị namespace — container có thể access host keyring nếu có đủ privilege.

**`/proc/sysrq-trigger`**: nếu không bị mask, container có thể trigger sysrq (reboot, OOM killer, etc.).

mydocker mask một số paths này:

```go
MaskedPaths: []string{
    "/proc/acpi", "/proc/kcore", "/proc/keys",
    "/proc/sched_debug", "/proc/scsi", "/sys/firmware",
}
ReadonlyPaths: []string{
    "/proc/bus", "/proc/fs", "/proc/irq",
    "/proc/sys", "/proc/sysrq-trigger",
}
```

---

## 3. Linux Capabilities — attack surface lớn nhất

### Capabilities là gì

Linux chia quyền root thành ~40 capabilities riêng lẻ. Thay vì "có root hoặc không", process có thể có một subset capabilities cụ thể.

### Capabilities mydocker giữ lại cho container

Từ `internal/oci/generate.go`:

```go
caps := []string{
    "CAP_CHOWN",           // chown files
    "CAP_DAC_OVERRIDE",    // bypass file permission checks
    "CAP_FSETID",          // set SUID/SGID bits
    "CAP_FOWNER",          // bypass ownership checks
    "CAP_MKNOD",           // create device files
    "CAP_NET_RAW",         // raw sockets, packet capture
    "CAP_SETGID",          // change GID
    "CAP_SETUID",          // change UID
    "CAP_SETFCAP",         // set file capabilities
    "CAP_SETPCAP",         // set process capabilities
    "CAP_NET_BIND_SERVICE", // bind ports < 1024
    "CAP_SYS_CHROOT",      // use chroot()
    "CAP_KILL",            // send signals to any process
    "CAP_AUDIT_WRITE",     // write audit log
}
```

### Capabilities nguy hiểm đang được giữ

**`CAP_NET_RAW`**: cho phép tạo raw sockets và packet capture. Container có thể sniff traffic trên bridge network, thực hiện ARP spoofing giữa các containers.

**`CAP_SYS_CHROOT`**: kết hợp với `CAP_MKNOD` và mount namespace không đủ kenh, có thể bị exploit.

**`CAP_SETUID` + `CAP_SETGID`**: container process có thể setuid(0) nếu có SUID binary trong container image — escalate về root.

**`CAP_FSETID`**: set SUID bit trên files — tạo SUID backdoor.

### Capabilities Docker thật drop thêm

Docker mặc định drop: `CAP_AUDIT_WRITE`, `CAP_NET_BROADCAST`, `CAP_SYS_PTRACE`, `CAP_SYS_MODULE`.

Docker không giữ: `CAP_SYS_ADMIN` (mount, sysctl, namespace operations, device access) — đây là capability nguy hiểm nhất.

### Implications cho mydocker

mydocker không drop capabilities đúng cách trong `internal/capability/cap.go` (placeholder). Container nhận bộ capability đầy đủ như root process bình thường.

---

## 4. Container Escape Vectors

### 4.1 Kernel exploit

Namespace isolation là kernel feature. Kernel vulnerability có thể bypass toàn bộ isolation. Đây là attack vector thực tế nhất và không thể mitigate ở application level.

**Mitigation production**: seccomp filter để giảm attack surface (chặn syscalls nguy hiểm), kernel version update.

mydocker không có seccomp filter.

### 4.2 Privileged container

Nếu container chạy với `CAP_SYS_ADMIN`, có thể mount host filesystem:

```bash
# Trong container với CAP_SYS_ADMIN
mount /dev/sda1 /mnt
# Đọc/ghi toàn bộ host filesystem
```

mydocker không implement `--privileged` flag — nhưng cũng không drop `CAP_SYS_ADMIN` đúng cách với tất cả containers.

### 4.3 `/proc/self/fd` escape

Nếu có file descriptor mở trỏ đến host filesystem trước khi `pivot_root`, container có thể dùng `/proc/self/fd/<n>` để access file đó sau pivot.

mydocker không close file descriptors trước reexec — đây là potential leak. runc đóng tất cả fd không cần thiết trước khi exec container process.

### 4.4 Shared kernel parameters

Container có thể đọc `/proc/sys/net/ipv4/ip_forward` và các kernel parameters. Trong một số trường hợp, thay đổi kernel parameter trong một network namespace ảnh hưởng host (tùy parameter và kernel version).

### 4.5 Time-of-check-time-of-use (TOCTOU) trong image extract

`internal/image/pull.go` extract tar layer với symlink support:

```go
case tar.TypeSymlink:
    os.Symlink(hdr.Linkname, target)
```

Malicious image có thể tạo symlink trỏ ra ngoài extract directory (path traversal). mydocker không validate symlink target:

```
# Malicious tar entry:
evil_symlink → ../../../../etc/cron.d
evil_symlink/malicious_file  ← ghi vào /etc/cron.d/malicious_file trên host
```

runc và Docker dùng `SecureJoin` để prevent symlink escape khi extract.

---

## 5. cgroup security

### Resource exhaustion (fork bomb)

Không có `--pids` limit, container có thể:

```bash
# Fork bomb trong container
:(){ :|:& };:
```

Tạo hàng nghìn processes, exhaust PID table của host, crash system.

```bash
# Với pids limit
mydocker run --pids=100 alpine /bin/sh
```

### Memory OOM behavior

Khi container vượt memory limit, kernel OOM killer chọn process để kill. OOM killer có thể chọn process **bên ngoài** container nếu container processes không phải victim tốt nhất (theo OOM score).

Production mitigation: set `memory.oom.group = 1` — kill toàn bộ cgroup thay vì process đơn lẻ. mydocker không set giá trị này.

### cgroup v1 vs v2 security

cgroup v1 có nhiều attack surfaces hơn: mỗi hierarchy riêng lẻ, có thể mount lại, device cgroup có thể bypass. cgroup v2 unified hierarchy an toàn hơn đáng kể. mydocker chỉ support cgroup v2 — đây là lựa chọn đúng.

---

## 6. Network security

### Container-to-container traffic

Tất cả containers dùng `--net` đều trên cùng bridge `mydocker0` — chúng có thể communicate trực tiếp không qua filtering. Không có network policy.

Docker Swarm và Kubernetes dùng per-network overlay hoặc network policy để isolate traffic giữa services.

### ARP spoofing

Do `CAP_NET_RAW`, container có thể gửi ARP replies giả mạo, impersonate container khác trên bridge. Không có ARP inspection trên bridge.

### Port scanning host

Container có thể scan `172.20.0.1` (bridge IP của host) và access services đang listen trên host.

---

## 7. So sánh security posture

| Security feature | mydocker | Docker (default) | gVisor | Kata Containers |
|-----------------|----------|------------------|--------|-----------------|
| Namespace isolation | ✓ | ✓ | ✓ | ✓ (VM) |
| cgroup limits | ✓ | ✓ | ✓ | ✓ |
| Seccomp filter | ✗ | ✓ (~300 syscalls) | N/A | ✓ |
| Capabilities drop | Minimal | Significant | N/A | Full (VM) |
| AppArmor/SELinux | ✗ | ✓ | ✗ | ✓ |
| Rootless | Partial | ✓ | ✓ | ✓ |
| Kernel shared | ✓ | ✓ | ✗ (guest kernel) | ✗ (VM kernel) |
| Symlink protection | ✗ | ✓ (SecureJoin) | ✓ | ✓ |
| fd leak prevention | ✗ | ✓ | ✓ | ✓ |

**gVisor** (Google): chạy container trong user-space kernel (ptrace hoặc KVM) — syscalls được intercept, không access host kernel trực tiếp. Overhead ~10-30% nhưng attack surface giảm dramatically.

**Kata Containers**: mỗi container là một lightweight VM — kernel riêng hoàn toàn. Escape container = phải escape VM. Overhead startup ~100ms nhưng isolation tương đương VM.

---

## 8. Rootless — trạng thái hiện tại

### Implement

`internal/container/rootless.go` có `SetupUserNamespace()` với `newuidmap`/`newgidmap`. Thêm `CLONE_NEWUSER` vào clone flags khi không chạy là root.

### Tại sao chưa hoàn chỉnh

Rootless cần:

1. **User namespace UID mapping**: root trong container (UID 0) map đến user UID trên host
2. **Rootless networking**: không thể tạo bridge, veth pair, iptables rules mà không có `CAP_NET_ADMIN`. Cần `slirp4netns` hoặc `pasta` làm userspace network stack
3. **Rootless cgroup**: cần cgroup delegation — user phải được delegated một subtree của cgroup hierarchy
4. **Rootless overlayfs**: kernel overlayfs cần `CAP_SYS_ADMIN`. Rootless cần `fuse-overlayfs` (FUSE-based implementation)

mydocker hiện tại chỉ xử lý được điểm 1 và 3 (một phần). Điểm 2 và 4 cần external tools.

### Production rootless

Docker rootless dùng: `rootlesskit` (namespace setup) + `slirp4netns` (network) + `fuse-overlayfs` (filesystem). Toàn bộ stack chạy trong user namespace.
