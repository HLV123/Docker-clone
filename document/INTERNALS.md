# Bên trong container — Kernel thực sự làm gì?

Tài liệu này giải thích cơ chế kỹ thuật bên trong container: namespace, cgroup, overlayfs, pivot_root. Đây là phần thú vị nhất — thứ mà Docker ẩn đi và mydocker expose ra.

---

## 1. Khi gõ `mydocker run alpine /bin/sh` — điều gì xảy ra?

```
Bước 1: mydocker (CLI) gọi container.Run()
Bước 2: Fork một child process với clone flags đặc biệt
Bước 3: Child process exec lại mydocker với argument "init"
Bước 4: mydocker init (chạy trong namespace mới) setup rootfs
Bước 5: mydocker init exec /bin/sh → trở thành PID 1 của container
```

Mỗi bước này có lý do kỹ thuật cụ thể. Đọc tiếp để hiểu tại sao.

---

## 2. Namespace — cô lập bằng cách thay đổi "góc nhìn"

### 2.1 Namespace là gì ở mức kernel

Khi một process được tạo ra (`fork`), nó mặc định thuộc về cùng namespace với process cha. Namespace không phải container — nó chỉ là một "cái kính" mà kernel dùng để trả lời câu hỏi của process.

```
Process hỏi: "Tôi có hostname gì?"
Kernel nhìn vào UTS namespace của process → trả lời

Process hỏi: "Các process đang chạy là gì?"
Kernel nhìn vào PID namespace của process → trả lời
```

Bằng cách cho process dùng namespace khác, kernel có thể trả lời khác nhau cho từng process.

### 2.2 Tạo namespace mới — `clone()` với flags

Linux syscall `clone()` (giống `fork()` nhưng mạnh hơn) cho phép tạo process mới trong namespace mới:

```go
// Trong mydocker — container/container.go
cmd.SysProcAttr = &syscall.SysProcAttr{
    Cloneflags: syscall.CLONE_NEWUTS |   // UTS namespace mới
                syscall.CLONE_NEWPID |   // PID namespace mới
                syscall.CLONE_NEWNS  |   // Mount namespace mới
                syscall.CLONE_NEWIPC |   // IPC namespace mới
                syscall.CLONE_NEWNET,    // Network namespace mới
}
```

Sau lệnh này, child process **đang sống trong một thế giới khác** — nó có namespace riêng cho mỗi loại tài nguyên.

### 2.3 PID namespace — process tưởng mình là PID 1

Thí nghiệm trực quan:

```bash
# Trên host
ps aux | head -5
# PID 1 = systemd (init process của Linux)
# PID 234 = sshd
# ...

# Trong container
mydocker run alpine /bin/sh -c "ps aux"
# PID 1 = /bin/sh  ← process của container tưởng mình là PID 1
```

Tại sao PID 1 quan trọng? Trong Linux, PID 1 là init process — có đặc quyền đặc biệt và nhận signal SIGCHLD từ tất cả orphan processes. Khi container process là PID 1, nó hoạt động như một init process độc lập.

### 2.4 Mount namespace — filesystem riêng

Khi tạo mount namespace mới (`CLONE_NEWNS`), child process nhận một bản copy của mount table của parent. Các mount/unmount sau đó của child không ảnh hưởng parent.

Nhưng có một vấn đề: mount propagation. Mặc định trên Ubuntu 22.04, mount points được share — mount trong namespace mới vẫn propagate ra host. Đó là lý do mydocker phải làm bước này đầu tiên:

```go
// container/init.go — bước đầu tiên trong Init()
syscall.Mount("", "/", "", syscall.MS_PRIVATE|syscall.MS_REC, "")
```

`MS_PRIVATE` ngắt kết nối propagation — mọi mount sau đó chỉ tồn tại trong namespace này.

### 2.5 UTS namespace — hostname riêng

UTS (Unix Time-sharing System) namespace cô lập hostname và domainname:

```go
// Sau khi vào UTS namespace mới
syscall.Sethostname([]byte("container-abc123"))
```

Thay đổi hostname này không ảnh hưởng host — chỉ process trong UTS namespace này thấy hostname mới.

### 2.6 Network namespace — network stack riêng

Network namespace tạo ra một network stack hoàn toàn độc lập: interfaces riêng, routing table riêng, iptables rules riêng.

```bash
# Trên host
ip addr show
# eth0: 192.168.1.100
# mydocker0: 172.20.0.1

# Trong container (sau khi setup)
ip addr show
# lo: 127.0.0.1
# veth-c-abc: 172.20.15.23  ← interface riêng của container
```

---

## 3. Reexec pattern — tại sao không exec thẳng?

Đây là điểm nhiều người thắc mắc nhất. Tại sao không làm:

```go
// Sai — không hoạt động
cmd.SysProcAttr = &syscall.SysProcAttr{Cloneflags: ...}
cmd.Run("/bin/sh")  // chạy thẳng shell của user
```

Mà phải làm:

```go
// Đúng — reexec pattern
cmd := exec.Command("/proc/self/exe", "init", containerID, ...)
cmd.SysProcAttr = &syscall.SysProcAttr{Cloneflags: ...}
cmd.Run()
// child sẽ exec lại binary mydocker với argument "init"
// sau đó mydocker init setup rootfs rồi mới exec /bin/sh
```

**Lý do:** Namespace mới được tạo ra khi `clone()` xảy ra — nhưng rootfs setup (overlayfs, pivot_root) cần chạy *bên trong* namespace mới, sau khi namespace đã được tạo. Không thể setup rootfs trước khi có namespace.

Giải pháp: child process exec lại chính nó với argument `init` → chạy trong namespace mới → setup rootfs → exec lệnh user muốn.

```
Parent process (namespace cũ)         Child process (namespace mới)
        │                                      │
        │  clone(CLONE_NEWNS|...)              │
        ├─────────────────────────────────────►│
        │                                      │
        │                             exec /proc/self/exe init
        │                                      │
        │                             [đang trong namespace mới]
        │                             MS_PRIVATE mount
        │                             mount overlayfs
        │                             pivot_root
        │                             mount /proc /sys /dev
        │                             exec /bin/sh
        │                                      │
        │  wait()                              │ [PID 1 của container]
        │◄─────────────────────────────────────┤
```

`/proc/self/exe` là symlink trỏ đến binary đang chạy — đây là cách reexec chính mình mà không cần biết path của binary.

---

## 4. Overlayfs — union filesystem

### 4.1 Vấn đề cần giải quyết

Nếu mỗi container cần filesystem riêng, cách đơn giản nhất là copy toàn bộ image. Nhưng:
- Alpine Linux = 8MB → 100 containers = 800MB chỉ để lưu filesystem
- Mỗi lần start container phải copy 8MB → chậm

### 4.2 Overlayfs giải quyết như thế nào

Overlayfs là **union filesystem** — hợp nhất nhiều directory thành một view duy nhất, với layer trên có thể ghi đè layer dưới:

```
Container thấy (merged):
  /bin/sh      ← từ lowerdir (image)
  /etc/hosts   ← từ upperdir (container tạo ra)
  /hello.txt   ← từ upperdir (container tạo ra)

lowerdir (image — read only):
  /bin/sh
  /etc/

upperdir (container — read/write):
  /etc/hosts   ← container đã modify /etc/hosts
  /hello.txt   ← container đã tạo file mới
```

**Quy tắc:**
- Đọc file → tìm trong upperdir trước, không có thì tìm lowerdir
- Ghi file → luôn ghi vào upperdir (copy-on-write)
- Xóa file từ lowerdir → tạo "whiteout file" trong upperdir

### 4.3 Trong code

```go
// container/init.go
opts := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s",
    imageLayersDir,   // read-only image
    containerUpper,   // read-write container layer
    containerWork,    // internal workdir (kernel dùng)
)
syscall.Mount("overlay", mergedDir, "overlay", 0, opts)
```

`workdir` là thư mục kernel dùng nội bộ để xử lý atomic operations — người dùng không cần quan tâm, nhưng bắt buộc phải có và phải trống.

### 4.4 Multi-layer image

Image nginx có 7 layers — mỗi layer là một bộ thay đổi:

```
Layer 7: entrypoint scripts          (top — đọc trước)
Layer 6: nginx config
Layer 5: nginx binary
Layer 4: dependencies
Layer 3: apt packages
Layer 2: apt lists
Layer 1: debian base OS              (bottom — đọc sau)
```

overlayfs cho phép chain nhiều lowerdir:

```
lowerdir=layer7:layer6:layer5:layer4:layer3:layer2:layer1
```

Kernel merge tất cả thành một view thống nhất, đọc từ trái sang phải (layer mới nhất ưu tiên hơn).

---

## 5. pivot_root — thay thế `/`

### 5.1 Tại sao không dùng chroot?

`chroot` là lệnh cũ để thay đổi root directory của process. Nhưng nó có lỗ hổng bảo mật: nếu có quyền đủ cao, process có thể `chroot` ra ngoài.

`pivot_root` là syscall kernel thay thế toàn bộ root filesystem, an toàn hơn và không thể escape bằng cách đơn giản.

### 5.2 pivot_root hoạt động như thế nào

```go
// container/init.go
func pivotRoot(newRoot string) error {
    // Bước 1: bind mount newRoot vào chính nó
    // (pivot_root yêu cầu newRoot phải là mount point)
    syscall.Mount(newRoot, newRoot, "", syscall.MS_BIND|syscall.MS_REC, "")

    // Bước 2: tạo thư mục để "đặt" old root vào
    os.MkdirAll(filepath.Join(newRoot, ".old_root"), 0700)

    // Bước 3: pivot!
    // newRoot trở thành /
    // old root (WSL2 filesystem) đi vào /.old_root
    syscall.PivotRoot(newRoot, filepath.Join(newRoot, ".old_root"))

    // Bước 4: chuyển vào / mới
    os.Chdir("/")

    // Bước 5: unmount old root — container không thể thấy host filesystem nữa
    syscall.Unmount("/.old_root", syscall.MNT_DETACH)
    os.Remove("/.old_root")
}
```

Sau pivot_root, container **không còn bất kỳ đường nào** để truy cập host filesystem.

### 5.3 Trước và sau pivot_root

```
Trước:                          Sau:
/  (WSL2 ext4)                  /  (Alpine overlayfs)
├── home/                       ├── bin/
│   └── hung/                   ├── etc/
│       └── projects/           ├── lib/
│           └── mydocker/       ├── proc/  ← mount mới
└── var/                        ├── sys/   ← mount mới
    └── lib/                    └── dev/   ← mount mới
        └── mydocker/
            └── containers/
                └── abc/
                    └── merged/  ← đây sẽ thành /
```

---

## 6. cgroup v2 — giới hạn tài nguyên

### 6.1 Cgroup là gì

cgroup (control groups) là cơ chế kernel để **nhóm các process lại** và **áp dụng giới hạn** cho cả nhóm: CPU, RAM, I/O, số process...

### 6.2 cgroup v1 vs v2

- **v1**: mỗi loại tài nguyên có hierarchy riêng (`/sys/fs/cgroup/cpu/`, `/sys/fs/cgroup/memory/`...)
- **v2**: unified hierarchy — tất cả trong `/sys/fs/cgroup/`, đơn giản hơn nhiều

mydocker dùng **cgroup v2** — Ubuntu 22.04 mặc định dùng v2.

### 6.3 Cách hoạt động — ghi vào file

Kernel expose cgroup qua filesystem `/sys/fs/cgroup/`. Để cấu hình cgroup, chỉ cần đọc/ghi file:

```bash
# Tạo cgroup cho container
mkdir /sys/fs/cgroup/mydocker/abc123

# Kernel tự tạo các file control trong thư mục đó:
ls /sys/fs/cgroup/mydocker/abc123
# cgroup.procs     ← danh sách PID trong group này
# memory.max       ← giới hạn memory
# cpu.max          ← giới hạn CPU
# pids.max         ← giới hạn số processes

# Set memory limit 100MB
echo "104857600" > /sys/fs/cgroup/mydocker/abc123/memory.max

# Set CPU limit 50%
echo "50000 100000" > /sys/fs/cgroup/mydocker/abc123/cpu.max
# "50000 microseconds mỗi 100000 microseconds" = 50%

# Add process vào cgroup
echo "1234" > /sys/fs/cgroup/mydocker/abc123/cgroup.procs
```

Sau khi add PID vào `cgroup.procs`, kernel tự động áp dụng giới hạn cho process đó và tất cả process con của nó.

### 6.4 Trong code

```go
// internal/cgroup/cgroup.go
func (c *Cgroup) Create() error {
    os.MkdirAll(c.path, 0755)
    // Bật controllers cho subtree
    os.WriteFile(parentSubtree, []byte("+memory +cpu +pids"), 0644)
    return nil
}

func (c *Cgroup) SetLimits(memBytes int64, cpuCores float64, pids int) error {
    os.WriteFile(c.path+"/memory.max", []byte(fmt.Sprintf("%d", memBytes)), 0644)
    os.WriteFile(c.path+"/cpu.max", []byte(fmt.Sprintf("%d 100000", quota)), 0644)
    os.WriteFile(c.path+"/pids.max", []byte(fmt.Sprintf("%d", pids)), 0644)
    return nil
}
```

---

## 7. Networking — veth pair và bridge

### 7.1 Bài toán

Container có network namespace riêng — nghĩa là network stack riêng hoàn toàn. Làm sao cho container giao tiếp với bên ngoài?

Không thể share interface với host vì sẽ phá vỡ namespace isolation.

### 7.2 Virtual Ethernet (veth) pair

`veth` là cặp network interface ảo — packet vào một đầu, ra đầu kia. Giống như một đường ống hai chiều.

```
Host side:  veth-h-abc  ←→  veth-c-abc  :Container side
            (trong host        (trong container
             namespace)         network namespace)
```

### 7.3 Bridge — switch ảo

`mydocker0` là bridge (switch ảo) trên host. Tất cả veth host-side được gắn vào bridge này:

```
Internet
    │
  eth0 (172.x.x.x)
    │
    │ iptables MASQUERADE (NAT)
    │
  mydocker0 bridge (172.20.0.1/16)
  ┌──────┬──────┬──────┐
  │      │      │      │
veth-h-1 veth-h-2 veth-h-3
  │      │      │
  │      │      │ (qua network namespace boundary)
  │      │      │
veth-c-1 veth-c-2 veth-c-3
(container1) (container2) (container3)
172.20.x.x  172.20.x.x  172.20.x.x
```

### 7.4 NAT — ra internet

Container muốn truy cập internet (IP 8.8.8.8):

```
Container (172.20.15.23) → veth-c → veth-h → mydocker0 → iptables MASQUERADE → eth0 → Internet
                                                           (đổi source IP thành eth0 IP)
```

Response đi ngược lại, iptables track connection và forward về đúng container.

### 7.5 Container DNS

```
/var/lib/mydocker/dns/hosts:
  172.20.15.23  webserver  #containerID-abc
  172.20.20.1   database   #containerID-xyz

Khi start container, file này được inject vào /etc/hosts của container
→ container có thể dùng "webserver" thay vì "172.20.15.23"
```

---

## 8. Toàn bộ flow — `mydocker run -it alpine /bin/sh`

```
1. mydocker parse arguments
   ↓
2. container.Run(cfg)
   ├── generateID() → "abc123"
   ├── os.MkdirAll("/var/lib/mydocker/containers/abc123/")
   ├── cgroup.Create() → mkdir /sys/fs/cgroup/mydocker/abc123
   ├── cgroup.SetLimits(memory, cpu, pids)
   ├── network.SetupBridge() → tạo mydocker0 nếu chưa có
   ├── veth.Create() → tạo veth pair
   └── exec.Command("/proc/self/exe", "init", "abc123", "alpine:latest", "/bin/sh")
       SysProcAttr.Cloneflags = CLONE_NEWUTS|CLONE_NEWPID|CLONE_NEWNS|CLONE_NEWIPC|CLONE_NEWNET
   ↓
3. kernel clone() → child process trong 5 namespaces mới
   ↓
4. child: exec /proc/self/exe "init" (reexec)
   ↓
5. container.Init() [chạy trong namespaces mới]
   ├── syscall.Mount("", "/", "", MS_PRIVATE|MS_REC, "")  ← private propagation
   ├── mount overlayfs (image layers + upper) → mergedDir
   ├── bind mount volumes vào mergedDir
   ├── copyResolvConf(mergedDir) → DNS
   ├── network.InjectDNS(mergedDir) → /etc/hosts
   ├── pivotRoot(mergedDir) → / = Alpine now
   ├── mount /proc /sys /dev /tmp /run
   ├── setupDevices() → /dev/null /dev/zero ...
   ├── syscall.Sethostname("container-abc123")
   └── syscall.Exec("/bin/sh", ["/bin/sh"], env)
       ← process này trở thành /bin/sh, không return
   ↓
6. parent: cgroup.AddProcess(child.PID)
   veth.MoveToNetns(child.PID) → đưa veth-c vào network namespace của container
   veth.SetupInsideContainer(child.PID) → nsenter, set IP, route
   state.Save({status: "running", PID: child.PID})
   ↓
7. User thấy shell của Alpine
   / #
```

---

## 9. Tại sao học những thứ này?

Hiểu container internals giúp bạn:

- **Debug tốt hơn**: khi container crash, bạn biết kiểm tra ở đâu (namespace? cgroup? filesystem?)
- **Bảo mật tốt hơn**: hiểu container escape là gì và cách phòng tránh
- **Thiết kế tốt hơn**: biết khi nào dùng container, khi nào dùng VM
- **Phỏng vấn**: đây là câu hỏi phổ biến trong phỏng vấn system engineer, DevOps, SRE
- **Nền tảng cho Kubernetes**: K8s chỉ là orchestrator trên cùng của container runtime
