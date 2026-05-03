# Qua project mydocker

Tài liệu này dành cho sinh viên muốn **học sâu về container** bằng cách đọc và hiểu source code mydocker. Không cần đọc tất cả cùng lúc — làm theo từng giai đoạn.

---

## Trước khi bắt đầu

Đảm bảo bạn đã đọc:
- `CONCEPTS.md` — hiểu container là gì, image là gì
- `INTERNALS.md` — hiểu namespace, cgroup, overlayfs hoạt động như thế nào

Và có kiến thức cơ bản về:
- Go (đọc được code, hiểu struct, goroutine)
- Linux command line cơ bản (`ls`, `mount`, `ps`, `ip`)
- Khái niệm về process, file descriptor, filesystem

---

## Giai đoạn 1 — Namespace và reexec

**Mục tiêu:** Hiểu container là một process đặc biệt, không phải máy ảo.

### File cần đọc

**`internal/container/container.go`** — hàm `Run()`

Tập trung vào:
```go
cmd.SysProcAttr = &syscall.SysProcAttr{
    Cloneflags: syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID | ...
}
```
Đây là điểm tạo ra namespace. Thử bỏ từng flag và xem điều gì thay đổi.

**`internal/container/init.go`** — hàm `Init()`

Đây là code chạy trong PID 1 của container. Đọc hiểu tại sao phải có bước `MS_PRIVATE` đầu tiên.

### Thực hành

```bash
# Thử tự tạo namespace bằng unshare (không cần code Go)
sudo unshare --pid --fork --mount-proc /bin/bash
# Bây giờ bạn đang trong PID namespace mới
echo $$        # sẽ là 1
ps aux         # chỉ thấy bash và ps
exit

# So sánh với host
ps aux         # thấy hàng trăm process
```

```bash
# Xem namespace của process đang chạy
ls -la /proc/self/ns/
# Mỗi file là một namespace — link đến namespace ID
```

```bash
# Khi container đang chạy, xem namespace của nó
ID=$(mydocker run -d alpine /bin/sh -c "sleep 60")
PID=$(cat /var/lib/mydocker/containers/$ID/state.json | python3 -c "import json,sys; print(json.load(sys.stdin)['pid'])")
ls -la /proc/$PID/ns/
# So sánh với /proc/self/ns/ — khác nhau = khác namespace
```

### Câu hỏi kiểm tra

1. Tại sao child process phải exec lại chính nó với argument "init" thay vì chạy thẳng lệnh user muốn?
2. Điều gì xảy ra nếu bỏ `CLONE_NEWNS`? Thử và quan sát.
3. Tại sao `MS_PRIVATE` phải là bước đầu tiên trong `Init()`?

---

## Giai đoạn 2 — Overlayfs và Filesystem

**Mục tiêu:** Hiểu container có filesystem riêng mà không cần copy toàn bộ image.

### File cần đọc

**`internal/container/rootfs.go`**

Đọc theo thứ tự:
1. `findImageBase()` — tìm image ở đâu trên disk
2. `buildLowerDir()` — tại sao có thể chain nhiều layers
3. Code mount overlayfs trong `init.go`
4. `pivotRoot()` — đọc comment từng bước

**`internal/image/pull.go`** — hàm `Pull()` và `extractTarGz()`

Hiểu cấu trúc của Docker Registry API: token → manifest → layers.

### Thực hành

```bash
# Tự mount overlayfs thủ công để hiểu
mkdir -p /tmp/overlay/{lower,upper,work,merged}
echo "from lower" > /tmp/overlay/lower/file.txt

sudo mount -t overlay overlay \
  -o lowerdir=/tmp/overlay/lower,upperdir=/tmp/overlay/upper,workdir=/tmp/overlay/work \
  /tmp/overlay/merged

# Xem merged — thấy file từ lower
cat /tmp/overlay/merged/file.txt

# Tạo file mới trong merged
echo "new file" > /tmp/overlay/merged/new.txt

# File mới xuất hiện ở upper, không phải lower
ls /tmp/overlay/upper/   # có new.txt
ls /tmp/overlay/lower/   # không có new.txt

# Modify file từ lower
echo "modified" > /tmp/overlay/merged/file.txt
ls /tmp/overlay/upper/   # file.txt xuất hiện ở upper (copy-on-write)
cat /tmp/overlay/lower/file.txt  # lower không bị thay đổi

sudo umount /tmp/overlay/merged
```

```bash
# Xem image layers trên disk
ls /var/lib/mydocker/images/nginx_latest/layers/
# Mỗi thư mục là một layer

# Xem nội dung layer đầu tiên (base OS)
ls /var/lib/mydocker/images/nginx_latest/layers/3531af2bc2a9/
```

```bash
# Khi container đang chạy, xem overlayfs mount
ID=$(mydocker run -d alpine /bin/sh -c "sleep 60")
cat /proc/mounts | grep overlay
# thấy lowerdir=...:...:... upperdir=... workdir=...
```

### Câu hỏi kiểm tra

1. Nếu container A và B cùng dùng nginx:latest, image có bị copy 2 lần không? Kiểm tra bằng `df -h` trước và sau khi chạy 2 containers.
2. Tạo file trong container rồi exit. File đó ở đâu trên host filesystem?
3. Tại sao `pivot_root` an toàn hơn `chroot`?

---

## Giai đoạn 3 — cgroup v2

**Mục tiêu:** Hiểu kernel giới hạn tài nguyên như thế nào.

### File cần đọc

**`internal/cgroup/cgroup.go`** — toàn bộ file

File này ngắn và đơn giản — cgroup chỉ là đọc/ghi file trong `/sys/fs/cgroup/`.

### Thực hành

```bash
# Tạo cgroup thủ công
sudo mkdir /sys/fs/cgroup/test-manual

# Kernel tự tạo các file control
ls /sys/fs/cgroup/test-manual/
# memory.max, cpu.max, pids.max, cgroup.procs, ...

# Set memory limit 50MB
sudo bash -c "echo 52428800 > /sys/fs/cgroup/test-manual/memory.max"

# Add shell hiện tại vào cgroup
sudo bash -c "echo $$ > /sys/fs/cgroup/test-manual/cgroup.procs"

# Kiểm tra đang trong cgroup nào
cat /proc/self/cgroup

# Thử vượt memory limit
dd if=/dev/zero of=/tmp/big bs=1M count=100
# Sẽ bị kill hoặc fail

# Cleanup
sudo rmdir /sys/fs/cgroup/test-manual
```

```bash
# Khi container đang chạy với --memory
ID=$(mydocker run -d --memory=64m alpine /bin/sh -c "sleep 60")

# Xem cgroup của container
cat /sys/fs/cgroup/mydocker/$ID/memory.max
# 67108864 = 64MB

cat /sys/fs/cgroup/mydocker/$ID/memory.current
# memory đang dùng
```

### Câu hỏi kiểm tra

1. Tại sao phải enable controller ở parent cgroup trước khi set limit ở child?
2. `cpu.max` có format `quota period` — `50000 100000` nghĩa là gì?
3. Điều gì xảy ra khi container vượt memory limit? OOM killer làm gì?

---

## Giai đoạn 4 — Networking

**Mục tiêu:** Hiểu container có IP riêng và giao tiếp với internet như thế nào.

### File cần đọc

**`internal/network/bridge.go`** — `SetupBridge()`

**`internal/network/veth.go`** — `Create()`, `MoveToNetns()`, `SetupInsideContainer()`

**`internal/network/portforward.go`** — iptables DNAT

**`internal/network/dns.go`** — container name resolution

### Thực hành

```bash
# Xem bridge trên host
ip link show mydocker0
ip addr show mydocker0
# Thấy IP 172.20.0.1/16

# Khi container đang chạy với --net, xem veth pair
ip link show | grep veth
# veth-h-abc: UP, BROADCAST

# Xem iptables rules
sudo iptables -t nat -L PREROUTING -n
sudo iptables -t nat -L POSTROUTING -n
# Thấy MASQUERADE rule cho 172.20.0.0/16
```

```bash
# Test container DNS
mydocker run -d --net --name server alpine /bin/sh -c "while true; do echo hi; sleep 1; done"
mydocker run --net alpine /bin/sh -c "cat /etc/hosts"
# Thấy "172.20.x.x  server  #containerID"

# Container gọi container khác bằng tên
mydocker run --net alpine /bin/sh -c "ping -c 3 server"
```

### Câu hỏi kiểm tra

1. Vẽ lại sơ đồ: packet đi từ container → internet qua bao nhiêu hops?
2. Port forwarding dùng iptables DNAT — DNAT là gì? Khác SNAT như thế nào?
3. Container DNS inject vào `/etc/hosts` — tại sao không dùng DNS server thật?

---

## Giai đoạn 5 — Image pull và Registry API

**Mục tiêu:** Hiểu Docker Hub lưu image như thế nào và pull hoạt động ra sao.

### File cần đọc

**`internal/image/pull.go`** — toàn bộ

Chú ý flow: `getToken()` → `getManifest()` → `downloadLayer()` → `extractTarGz()`

### Thực hành

```bash
# Tự gọi Docker Registry API bằng curl
# Bước 1: lấy token
TOKEN=$(curl -s "https://auth.docker.io/token?service=registry.docker.io&scope=repository:library/alpine:pull" | python3 -c "import json,sys; print(json.load(sys.stdin)['token'])")

# Bước 2: lấy manifest
curl -s -H "Authorization: Bearer $TOKEN" \
  -H "Accept: application/vnd.docker.distribution.manifest.v2+json" \
  "https://registry-1.docker.io/v2/library/alpine/manifests/latest" \
  | python3 -m json.tool

# Thấy: config digest và layers digest
```

```bash
# Xem image đã pull trên disk
ls /var/lib/mydocker/images/alpine_latest/
# manifest.json  config.json  layers/

cat /var/lib/mydocker/images/alpine_latest/config.json
# Thấy: Entrypoint, Cmd, Env, WorkingDir
```

### Câu hỏi kiểm tra

1. Tại sao manifest list (multi-arch) cần được xử lý khác với manifest thường?
2. SHA256 digest dùng để làm gì khi download layer?
3. Whiteout file `.wh.foo` nghĩa là gì? Tại sao cần khi có overlayfs?

---

## Giai đoạn 6 — Daemon và API

**Mục tiêu:** Hiểu kiến trúc client-server, Unix socket, HTTP API.

### File cần đọc

**`internal/api/types.go`** — request/response types

**`internal/api/server.go`** — HTTP server, routing

**`internal/api/client.go`** — HTTP client

**`cmd/mydockerd/main.go`** — daemon implementation

### Thực hành

```bash
# Khi mydockerd đang chạy, gọi API trực tiếp bằng curl
sudo mydockerd &
sleep 1

# Ping daemon
curl --unix-socket /var/run/mydocker.sock http://daemon/ping

# List containers
curl --unix-socket /var/run/mydocker.sock http://daemon/containers?all=true | python3 -m json.tool

# Tạo container qua API
curl --unix-socket /var/run/mydocker.sock \
  -X POST \
  -H "Content-Type: application/json" \
  -d '{"image": "alpine:latest", "command": ["/bin/sh", "-c", "echo hello"]}' \
  http://daemon/containers
```

### Câu hỏi kiểm tra

1. Tại sao dùng Unix socket thay vì TCP port?
2. Restart policy được implement như thế nào trong daemon? (goroutine `watchContainer`)
3. Khi daemon crash, containers đang chạy thì sao? Tại sao?

---

## Giai đoạn 7 — Build và Compose

**Mục tiêu:** Hiểu Dockerfile được execute như thế nào, và compose orchestrate containers ra sao.

### File cần đọc

**`internal/build/dockerfile.go`** — Dockerfile parser

**`internal/build/builder.go`** — builder, mỗi instruction tạo layer

**`internal/compose/parser.go`** — docker-compose.yml parser, `topoSort()`

**`internal/compose/runner.go`** — start services theo đúng thứ tự

### Thực hành

```bash
# Tạo Dockerfile phức tạp hơn để test parser
cat > /tmp/test.Dockerfile << 'EOF'
FROM alpine:latest
ARG VERSION=1.0
ENV APP_VERSION=$VERSION
RUN echo "building version $APP_VERSION"
WORKDIR /app
COPY . /app/
RUN ls -la /app
CMD ["/bin/sh", "-c", "echo done"]
EOF

mydocker build -t test:v1 -f /tmp/test.Dockerfile /tmp
```

```bash
# Test depends_on ordering
cat > /tmp/compose-test.yml << 'EOF'
version: "3"
services:
  c:
    image: alpine:latest
    command: /bin/sh -c "echo C started && sleep 30"
    depends_on: [b]
  a:
    image: alpine:latest
    command: /bin/sh -c "echo A started && sleep 30"
  b:
    image: alpine:latest
    command: /bin/sh -c "echo B started && sleep 30"
    depends_on: [a]
EOF

# A phải start trước B, B trước C
sudo mydocker-compose -f /tmp/compose-test.yml up -d
sudo mydocker ps   # xem thứ tự created_at
```

---

## Bài tập tự làm

Những bài tập này giúp củng cố hiểu biết bằng cách tự viết code:

### Bài 1 — Thêm `--dns` flag

Cho phép user chỉ định DNS server cho container:
```bash
mydocker run --dns=8.8.8.8 alpine /bin/sh -c "nslookup google.com"
```

*Gợi ý:* Modify `copyResolvConf()` trong `rootfs.go` để ghi `nameserver 8.8.8.8` vào `/etc/resolv.conf`.

### Bài 2 — Thêm `mydocker top`

Hiển thị processes đang chạy trong container:
```bash
mydocker top <container-id>
```

*Gợi ý:* Dùng `nsenter` để chạy `ps aux` trong namespace của container.

### Bài 3 — Thêm `mydocker cp`

Copy file giữa host và container:
```bash
mydocker cp mycontainer:/etc/nginx/nginx.conf ./nginx.conf
mydocker cp ./myfile.txt mycontainer:/tmp/
```

*Gợi ý:* Container đang chạy → dùng `/proc/<pid>/root/` để access filesystem. Container đã exit → dùng `upper/` directory.

### Bài 4 — Thêm `--read-only` flag

Container chỉ có thể đọc filesystem, không ghi:
```bash
mydocker run --read-only alpine /bin/sh
# Thử tạo file: sẽ bị từ chối
```

*Gợi ý:* Modify overlayfs mount để không có `upperdir`, hoặc remount merged với `MS_RDONLY`.

### Bài 5 — Implement `mydocker diff`

Hiển thị files đã thay đổi trong container so với image:
```bash
mydocker diff <container-id>
# C /etc/hosts    ← Changed
# A /hello.txt    ← Added
# D /tmp/cache    ← Deleted
```

*Gợi ý:* Walk qua `upper/` directory. Files bình thường = Added/Changed. Whiteout files (`.wh.*`) = Deleted.

---

## Tài liệu tham khảo theo thứ tự

1. **"Containers from Scratch"** — Liz Rice (YouTube, ~40 phút)
   Xem trước khi đọc code. Cô ấy live-code container bằng Go từ đầu, rất trực quan.
   https://www.youtube.com/watch?v=8fi7uSYlOdc

2. **`man 7 namespaces`** — overview tất cả namespace types
   ```bash
   man 7 namespaces
   ```

3. **`man 2 clone`** — syscall tạo process với namespace mới
   ```bash
   man 2 clone
   ```

4. **`man 2 pivot_root`** — syscall thay đổi root filesystem
   ```bash
   man 2 pivot_root
   ```

### Kernel documentation

- **overlayfs**: `Documentation/filesystems/overlayfs.rst`
  https://www.kernel.org/doc/html/latest/filesystems/overlayfs.html

- **cgroup v2**: `Documentation/admin-guide/cgroup-v2.rst`
  https://www.kernel.org/doc/html/latest/admin-guide/cgroup-v2.html

### Source code tham khảo

- **runc** — production OCI runtime (Docker dùng bên dưới)
  https://github.com/opencontainers/runc
  Đọc: `libcontainer/container_linux.go`, `libcontainer/init_linux.go`

- **Docker Registry API v2**
  https://docs.docker.com/registry/spec/api/

### Sách

- **"Container Security"** — Liz Rice (O'Reilly, miễn phí online)
  https://www.oreilly.com/library/view/container-security/9781492056690/

- **"Linux Kernel Development"** — Robert Love
  Chương về process management, filesystem, networking

---
