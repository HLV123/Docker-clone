# Hướng dẫn sử dụng mydocker

---

## Mục lục

1. [mydocker — CLI cơ bản](#1-mydocker--cli-cơ-bản)
2. [Pull image](#2-pull-image)
3. [Run container](#3-run-container)
4. [Build image từ Dockerfile](#4-build-image-từ-dockerfile)
5. [Quản lý container](#5-quản-lý-container)
6. [Networking](#6-networking)
7. [Volume — lưu trữ dữ liệu](#7-volume--lưu-trữ-dữ-liệu)
8. [Resource limits](#8-resource-limits)
9. [Daemon — mydockerd](#9-daemon--mydockerd)
10. [docker-compose — mydocker-compose](#10-docker-compose--mydocker-compose)
11. [OCI compatibility với runc](#11-oci-compatibility-với-runc)
12. [Ví dụ thực tế end-to-end](#12-ví-dụ-thực-tế-end-to-end)

---

## 1. mydocker — CLI cơ bản

`mydocker` là lệnh chính để tương tác với container runtime. Khi daemon `mydockerd` đang chạy, các lệnh sẽ đi qua daemon. Khi không có daemon, mydocker chạy trực tiếp (direct mode).

Xem danh sách lệnh:

```bash
mydocker
```

```
Commands:
  run       Run a container
  build     Build image from Dockerfile
  pull      Pull image from Docker Hub
  images    List images
  ps        List containers
  stop      Stop container
  rm        Remove container
  exec      Execute in running container
  logs      Fetch container logs
  stats     Show resource usage
  inspect   Show detailed info
  spec      Show OCI config.json
  runtime   Show available OCI runtime
```

---

## 2. Pull image

Tải image từ Docker Hub về máy.

### Cú pháp

```bash
mydocker pull <image>:<tag>
```

### Ví dụ

```bash
# Pull Alpine Linux (nhỏ gọn, ~8MB)
mydocker pull alpine:latest

# Pull Alpine phiên bản cụ thể
mydocker pull alpine:3.18

# Pull Nginx web server
mydocker pull nginx:latest
```

### Xem danh sách images đã pull

```bash
mydocker images
```

```
REPOSITORY   TAG      LAYERS   SIZE
alpine       latest   1        8.1 MB
alpine       3.18     1        7.0 MB
nginx        latest   7        157.2 MB
```

### Lưu ý

- Image được lưu tại `/var/lib/mydocker/images/`
- Nếu layer đã tồn tại (theo SHA256), sẽ không download lại
- Chỉ support public image từ Docker Hub, không cần đăng nhập

---

## 3. Run container

Lệnh quan trọng nhất. Tạo và chạy container từ image.

### Cú pháp

```bash
mydocker run [options] <image> [command]
```

---

### 3.1 Chạy cơ bản

```bash
# Chạy và vào shell Alpine
mydocker run alpine:latest

# Chạy lệnh cụ thể rồi thoát
mydocker run alpine:latest /bin/echo "Hello World"

# Chạy shell script
mydocker run alpine:latest /bin/sh -c "echo hello && date && hostname"
```

---

### 3.2 Interactive + TTY (`-it`)

Dùng khi muốn tương tác trực tiếp với container, ví dụ vào shell.

```bash
mydocker run -it alpine:latest
```

Sẽ vào shell bên trong container:

```
/ # ls
bin    dev    etc    home   lib    media  mnt    opt    proc
/ # hostname
container-abc123
/ # cat /etc/os-release
NAME="Alpine Linux"
/ # exit
```

Flags:
- `-i` — giữ stdin mở (interactive)
- `-t` — cấp PTY (terminal)
- `-it` — kết hợp cả hai, dùng phổ biến nhất

---

### 3.3 Detach — chạy background (`-d`)

Container chạy nền, terminal không bị chiếm.

```bash
mydocker run -d alpine:latest /bin/sh -c "while true; do echo running; sleep 5; done"
```

Output là container ID:
```
a1b2c3d4-e5f6
```

Dùng ID này để quản lý container sau:
```bash
mydocker ps               # xem đang chạy không
mydocker logs a1b2c3d4-e5f6   # xem output
mydocker stop a1b2c3d4-e5f6   # dừng lại
```

---

### 3.4 Đặt tên container (`--name`)

Thay vì dùng ID ngẫu nhiên, đặt tên dễ nhớ.

```bash
mydocker run -d --name webserver nginx:latest
mydocker run -d --name database alpine:latest /bin/sh -c "sleep infinity"
```

Dùng tên để thao tác:
```bash
mydocker stop webserver
mydocker logs webserver
mydocker exec webserver /bin/sh
```

---

### 3.5 Đặt hostname (`--hostname`)

Hostname bên trong container, mặc định là `container-<id[:6]>`.

```bash
mydocker run -it --hostname myserver alpine:latest
```

```
/ # hostname
myserver
```

---

### 3.6 Environment variables (`-e`)

Truyền biến môi trường vào container.

```bash
# Truyền 1 biến
mydocker run -e APP_ENV=production alpine:latest /bin/sh -c "echo $APP_ENV"

# Truyền nhiều biến
mydocker run \
  -e DATABASE_URL=postgres://localhost/mydb \
  -e SECRET_KEY=abc123 \
  -e DEBUG=false \
  alpine:latest /bin/sh -c "env | grep -E 'DATABASE|SECRET|DEBUG'"
```

---

### 3.7 Working directory (`-w`)

Thư mục làm việc mặc định bên trong container.

```bash
mydocker run -w /app alpine:latest /bin/sh -c "pwd"
# Output: /app
```

---

### 3.8 Entrypoint override (`--entrypoint`)

Ghi đè entrypoint mặc định của image.

```bash
# Thay vì chạy entrypoint mặc định của nginx, vào shell
mydocker run -it --entrypoint /bin/sh nginx:latest
```

---

### 3.9 Restart policy (`--restart`)

Tự động restart container khi bị exit hoặc crash. **Yêu cầu daemon đang chạy.**

```bash
# Luôn restart (kể cả khi stop thủ công)
mydocker run -d --restart=always --name web nginx:latest

# Chỉ restart khi exit code khác 0 (crash)
mydocker run -d --restart=on-failure --name api alpine:latest /bin/sh -c "exit 1"

# Restart trừ khi stop thủ công
mydocker run -d --restart=unless-stopped --name db alpine:latest /bin/sh -c "sleep infinity"
```

Kiểm tra restart policy hoạt động:
```bash
# Container sẽ tự restart sau vài giây
mydocker run -d --restart=always --name test alpine:latest /bin/sh -c "sleep 3"
sleep 5
mydocker ps | grep test   # vẫn running
```

---

## 4. Build image từ Dockerfile

`mydocker build` đọc Dockerfile, chạy từng instruction và tạo image mới.

### Cú pháp

```bash
mydocker build -t <name>:<tag> [context_directory]
```

---

### 4.1 Dockerfile instructions được hỗ trợ

| Instruction | Mô tả | Ví dụ |
|-------------|-------|-------|
| `FROM` | Base image | `FROM alpine:latest` |
| `RUN` | Chạy lệnh khi build | `RUN apk add curl` |
| `COPY` | Copy file từ host vào image | `COPY app.sh /app/` |
| `ADD` | Giống COPY, hỗ trợ URL | `ADD config.tar.gz /etc/` |
| `ENV` | Set environment variable | `ENV APP_PORT=8080` |
| `WORKDIR` | Set thư mục làm việc | `WORKDIR /app` |
| `CMD` | Lệnh mặc định khi run | `CMD ["/bin/sh"]` |
| `ENTRYPOINT` | Entrypoint của container | `ENTRYPOINT ["/app/start.sh"]` |
| `EXPOSE` | Khai báo port (metadata) | `EXPOSE 80` |
| `ARG` | Build argument | `ARG VERSION=1.0` |
| `LABEL` | Metadata | `LABEL version="1.0"` |

---

### 4.2 Ví dụ — shell script đơn giản

```bash
mkdir -p ~/myapp && cd ~/myapp
```

Tạo `app.sh`:
```bash
cat > app.sh << 'EOF'
#!/bin/sh
echo "=== My App ==="
echo "Version: $APP_VERSION"
echo "Env: $APP_ENV"
echo "Time: $(date)"
EOF
chmod +x app.sh
```

Tạo `Dockerfile`:
```dockerfile
FROM alpine:latest

# Cài tools cần thiết
RUN apk add --no-cache curl

# Set environment
ENV APP_ENV=production
ENV APP_VERSION=1.0.0

# Set working dir
WORKDIR /app

# Copy source code
COPY app.sh /app/app.sh

# Command mặc định
CMD ["/bin/sh", "/app/app.sh"]
```

Build và chạy:
```bash
mydocker build -t myapp:v1 .
mydocker run myapp:v1
```

Output:
```
=== My App ===
Version: 1.0.0
Env: production
Time: Sun May  3 15:00:00 UTC 2026
```

---

### 4.3 Ví dụ — web server tĩnh

```bash
mkdir -p ~/webapp/html && cd ~/webapp

echo "<h1>Hello from mydocker!</h1>" > html/index.html

cat > Dockerfile << 'EOF'
FROM nginx:latest
COPY html/ /usr/share/nginx/html/
EOF

mydocker build -t webapp:v1 .
mydocker run -d -p 8080:80 --net --name webapp webapp:v1
sleep 2

IP=$(mydocker ps | grep webapp | awk '{print $7}')
curl http://$IP
```

---

### 4.4 Build với custom Dockerfile path (`-f`)

```bash
mydocker build -t myapp:v1 -f docker/Dockerfile.prod .
```

---

## 5. Quản lý container

### 5.1 Liệt kê containers

```bash
# Chỉ xem containers đang chạy
mydocker ps

# Xem tất cả (cả đã exit)
mydocker ps -a
```

Output:
```
CONTAINER ID   NAME        IMAGE          COMMAND   STATUS    PORTS          IP              CREATED
a1b2c3d4-e5f   webserver   nginx:latest   /docker   running   8080->80/tcp   172.20.15.23    2026-05-03 15:00:00
b2c3d4e5-f6a   -           alpine:latest  /bin/sh   exited    -                              2026-05-03 14:55:00
```

---

### 5.2 Dừng container

```bash
# Graceful stop: gửi SIGTERM, chờ 10 giây, rồi SIGKILL
mydocker stop webserver

# Dừng nhiều container cùng lúc
mydocker stop web db api
```

---

### 5.3 Xóa container

```bash
# Xóa container đã exit
mydocker rm a1b2c3d4-e5f

# Xóa container đang chạy (force kill)
mydocker rm -f webserver

# Xóa tất cả containers đã exit
mydocker ps -a | tail -n +2 | awk '{print $1}' | xargs -I{} mydocker rm {}
```

---

### 5.4 Exec — vào container đang chạy

```bash
# Vào shell của container đang chạy
mydocker exec webserver /bin/sh

# Chạy lệnh cụ thể không vào shell
mydocker exec webserver /bin/sh -c "nginx -t"
mydocker exec webserver /bin/sh -c "cat /etc/nginx/nginx.conf"
```

---

### 5.5 Logs — xem output

```bash
# Xem toàn bộ logs (container phải chạy với -d)
mydocker logs webserver

# Xem 10 dòng cuối
mydocker logs webserver | tail -10
```

---

### 5.6 Stats — xem tài nguyên

```bash
mydocker stats webserver
```

Output:
```
Container: a1b2c3d4-e5f6
Status:    running (PID 1234)
Memory:    12.5 MB / 100.0 MB
CPU:       245ms
```

---

### 5.7 Inspect — xem chi tiết

```bash
# Inspect container
mydocker inspect a1b2c3d4-e5f6

# Inspect image config (Entrypoint, Cmd, Env)
mydocker inspect nginx:latest
```

Output (image):
```json
{
  "Entrypoint": ["/docker-entrypoint.sh"],
  "Cmd": ["nginx", "-g", "daemon off;"],
  "Env": ["PATH=/usr/local/sbin:...", "NGINX_VERSION=1.29.8"],
  "WorkingDir": ""
}
```

---

## 6. Networking

### 6.1 Bật network cho container

Mặc định container không có network. Dùng `--net` để bật.

```bash
mydocker run --net alpine:latest /bin/sh -c "wget -q -O- http://example.com | head -3"
```

Khi bật `--net`:
- Container được cấp IP trong dải `172.20.0.0/16`
- Container có thể truy cập internet qua NAT
- Các container cùng bridge có thể gọi nhau

---

### 6.2 Port forwarding (`-p`)

Map port từ host vào container.

```bash
# host:container
mydocker run -d --net -p 8080:80 nginx:latest

# Nhiều port
mydocker run -d --net -p 8080:80 -p 8443:443 nginx:latest

# Chỉ định protocol
mydocker run -d --net -p 5353:53/udp alpine:latest
```

Sau đó truy cập từ host:
```bash
# Lấy IP container
IP=$(mydocker ps | grep <name> | awk '{print $7}')
curl http://$IP:80

# Hoặc dùng port forwarding
curl http://localhost:8080   # chỉ hoạt động trên Linux native, không qua WSL2
```

**Lưu ý WSL2:** Do cấu trúc mạng của WSL2, `localhost:8080` từ Windows không forward vào container. Dùng IP của container trực tiếp.

---

### 6.3 Container DNS — gọi nhau bằng tên

Khi nhiều container chạy với `--net` và `--name`, chúng có thể gọi nhau bằng tên thay vì IP.

```bash
# Terminal 1: chạy web server
mydocker run -d --net --name webserver nginx:latest

# Terminal 2: chạy container khác
mydocker run --net alpine:latest /bin/sh -c "wget -q -O- http://webserver | head -3"
```

Container thứ hai tự resolve `webserver` thành IP — không cần biết IP cụ thể.

DNS entries được lưu tại `/var/lib/mydocker/dns/hosts` và inject vào `/etc/hosts` của mỗi container.

---

### 6.4 Tắt network (`--no-net`)

```bash
mydocker run --no-net alpine:latest /bin/sh -c "ip addr"
# Chỉ thấy loopback, không có network interface
```

---

## 7. Volume — lưu trữ dữ liệu

Volume cho phép dữ liệu tồn tại sau khi container exit, hoặc chia sẻ file giữa host và container.

### 7.1 Bind mount — mount thư mục từ host

```bash
# Cú pháp: -v <host_path>:<container_path>
mydocker run -v /tmp/mydata:/data alpine:latest /bin/sh -c "ls /data"

# Relative path (tính từ thư mục hiện tại)
mkdir -p ./data
echo "hello" > ./data/test.txt
mydocker run -v ./data:/app/data alpine:latest /bin/sh -c "cat /app/data/test.txt"
```

---

### 7.2 Read-only volume

```bash
mydocker run -v ./config:/etc/myapp:ro alpine:latest /bin/sh -c "cat /etc/myapp/config.yaml"
# Container không thể ghi vào /etc/myapp
```

---

### 7.3 Nhiều volumes

```bash
mydocker run \
  -v ./html:/usr/share/nginx/html \
  -v ./config/nginx.conf:/etc/nginx/nginx.conf:ro \
  -v ./logs:/var/log/nginx \
  --net -p 8080:80 \
  nginx:latest
```

---

### 7.4 Persistent data — ví dụ thực tế

```bash
# Tạo thư mục lưu data
mkdir -p ~/mydata

# Ghi data vào container
mydocker run -v ~/mydata:/data alpine:latest /bin/sh -c "echo 'persistent data' > /data/file.txt"

# Dữ liệu vẫn còn sau khi container exit
cat ~/mydata/file.txt
# Output: persistent data

# Container mới vẫn đọc được data cũ
mydocker run -v ~/mydata:/data alpine:latest /bin/sh -c "cat /data/file.txt"
```

---

## 8. Resource limits

Giới hạn tài nguyên để tránh một container chiếm hết CPU/RAM của host.

### 8.1 Memory limit (`--memory`)

```bash
# Giới hạn 100MB
mydocker run --memory=100m alpine:latest /bin/sh

# Giới hạn 1GB
mydocker run --memory=1g alpine:latest /bin/sh

# Giới hạn với đơn vị bytes
mydocker run --memory=104857600 alpine:latest /bin/sh
```

Đơn vị: `k` (kilobytes), `m` (megabytes), `g` (gigabytes).

Test memory limit:
```bash
# Container này sẽ bị OOM kill khi cố dùng >50MB
mydocker run --memory=50m alpine:latest /bin/sh -c "dd if=/dev/zero of=/tmp/big bs=1M count=100"
```

---

### 8.2 CPU limit (`--cpus`)

```bash
# Giới hạn 0.5 CPU (50% của 1 core)
mydocker run --cpus=0.5 alpine:latest /bin/sh

# Giới hạn 2 CPUs
mydocker run --cpus=2 alpine:latest /bin/sh
```

---

### 8.3 Process limit (`--pids`)

Giới hạn số process tối đa bên trong container (ngăn fork bomb).

```bash
# Tối đa 20 processes
mydocker run --pids=20 alpine:latest /bin/sh
```

---

### 8.4 Kết hợp nhiều limits

```bash
mydocker run \
  --memory=256m \
  --cpus=0.5 \
  --pids=50 \
  -d --name limited-app \
  alpine:latest /bin/sh -c "sleep infinity"

# Xem resource usage
mydocker stats limited-app
```

---

## 9. Daemon — mydockerd

Daemon là process chạy nền, quản lý toàn bộ lifecycle của containers. Cần thiết cho restart policy và một số tính năng nâng cao.

### 9.1 Khởi động daemon

```bash
# Chạy foreground (xem log trực tiếp)
sudo mydockerd

# Chạy background
sudo mydockerd &

# Chạy và lưu log
sudo mydockerd > /var/log/mydockerd.log 2>&1 &
```

Output khi start:
```
Starting mydockerd...
Recovered 2 running containers
mydockerd listening on /var/run/mydocker.sock
```

---

### 9.2 Kiểm tra daemon đang chạy

```bash
ls /var/run/mydocker.sock   # socket tồn tại = daemon đang chạy
mydocker ps                  # nếu không báo lỗi = daemon OK
```

---

### 9.3 Dừng daemon

```bash
# Graceful shutdown
sudo kill $(pgrep mydockerd)

# Hoặc
sudo pkill -f mydockerd.real
```

---

### 9.4 Daemon mode vs Direct mode

| | Daemon mode | Direct mode |
|--|------------|-------------|
| Yêu cầu | `mydockerd` đang chạy | Không cần |
| `--restart` policy | Hoạt động | Không hoạt động |
| Detach (`-d`) | Daemon quản lý | Process tự quản lý |
| Auto-recover | Có (khi daemon restart) | Không |
| Interactive (`-it`) | Direct mode | Direct mode |

**Lưu ý:** Lệnh interactive (`-it`) luôn dùng direct mode dù daemon có chạy hay không.

---

### 9.5 Restart policy với daemon

```bash
sudo mydockerd &
sleep 1

# Luôn restart kể cả khi stop thủ công
mydocker run -d --restart=always --name web nginx:latest

# Chỉ restart khi crash (exit code != 0)
mydocker run -d --restart=on-failure --name api alpine:latest /bin/sh -c "exit 1"

# Restart trừ khi stop bằng mydocker stop
mydocker run -d --restart=unless-stopped --name db alpine:latest /bin/sh -c "sleep infinity"
```

Xem container tự restart:
```bash
mydocker run -d --restart=always --name test alpine:latest /bin/sh -c "sleep 3 && exit 0"
watch -n 1 "mydocker ps | grep test"
# Container sẽ restart mỗi ~3 giây
```

---

## 10. docker-compose — mydocker-compose

`mydocker-compose` đọc file `docker-compose.yml` và quản lý nhiều containers như một ứng dụng.

### 10.1 Cấu trúc docker-compose.yml

```yaml
version: "3"

services:
  web:                          # tên service
    image: nginx:latest         # image sử dụng
    ports:
      - "8080:80"               # port forwarding
    volumes:
      - ./html:/usr/share/nginx/html   # volume mount
    restart: always             # restart policy
    depends_on:
      - api                     # chờ api start trước

  api:
    image: alpine:latest
    command: /bin/sh -c "while true; do echo 'API running'; sleep 10; done"
    environment:
      - APP_ENV=production       # list format
      DATABASE_URL: postgres://db/myapp   # map format
    depends_on:
      - db

  db:
    image: alpine:latest
    command: ["/bin/sh", "-c", "sleep infinity"]   # exec format
    volumes:
      - dbdata:/var/lib/data    # named volume

volumes:
  dbdata:                       # khai báo named volume
```

---

### 10.2 Các lệnh mydocker-compose

#### Khởi động tất cả services

```bash
# Foreground (xem log trực tiếp)
sudo mydocker-compose up

# Background (detached)
sudo mydocker-compose up -d
```

Services sẽ start theo thứ tự `depends_on`.

---

#### Xem trạng thái

```bash
sudo mydocker-compose ps
```

```
NAME                 IMAGE           STATUS     PORTS
example_web          nginx:latest    running    8080->80
example_api          alpine:latest   running    -
example_db           alpine:latest   running    -
```

---

#### Xem logs

```bash
# Logs tất cả services
sudo mydocker-compose logs

# Logs service cụ thể
sudo mydocker-compose logs web
sudo mydocker-compose logs api
```

---

#### Exec vào service

```bash
sudo mydocker-compose exec web /bin/sh
sudo mydocker-compose exec api /bin/sh -c "env"
```

---

#### Dừng và xóa

```bash
# Dừng và xóa tất cả containers
sudo mydocker-compose down
```

---

#### Dùng file compose khác

```bash
sudo mydocker-compose -f docker-compose.prod.yml up -d
```

---

### 10.3 Ví dụ thực tế — web + api stack

```bash
mkdir -p ~/webapp/html && cd ~/webapp

echo "<h1>Hello from compose!</h1>" > html/index.html

cat > docker-compose.yml << 'EOF'
version: "3"

services:
  web:
    image: nginx:latest
    ports:
      - "8080:80"
    volumes:
      - ./html:/usr/share/nginx/html
    restart: always

  monitor:
    image: alpine:latest
    command: /bin/sh -c "while true; do echo '[monitor] web is up'; sleep 30; done"
    depends_on:
      - web
EOF

sudo mydocker-compose up -d
sleep 3
sudo mydocker-compose ps

WEB_IP=$(mydocker ps | grep webapp_web | awk '{print $7}')
curl http://$WEB_IP

sudo mydocker-compose logs monitor
sudo mydocker-compose down
```

---

## 11. OCI compatibility với runc

mydocker tạo file `config.json` theo chuẩn OCI Runtime Spec cho mỗi container. File này có thể dùng với `runc` — production container runtime của Docker.

### 11.1 Xem OCI spec của container

```bash
# Chạy container trước
ID=$(mydocker run -d alpine:latest /bin/sh -c "sleep 60")
sleep 1

# Xem OCI config.json
mydocker spec $ID
```

Output (rút gọn):
```json
{
  "ociVersion": "1.0.2",
  "hostname": "container-abc123",
  "process": {
    "args": ["/bin/sh", "-c", "sleep 60"],
    "env": ["PATH=/usr/local/sbin:..."],
    "cwd": "/"
  },
  "root": {
    "path": "/var/lib/mydocker/containers/abc123/merged"
  },
  "linux": {
    "namespaces": [
      {"type": "pid"},
      {"type": "network"},
      {"type": "ipc"},
      {"type": "uts"},
      {"type": "mount"}
    ],
    "cgroupsPath": "/mydocker/abc123"
  }
}
```

---

### 11.2 Xem runtime đang dùng

```bash
mydocker runtime
```

Output nếu `runc` đã cài:
```
Runtime: runc
Available: true
```

Output nếu chưa cài:
```
Runtime: mydocker-native
Available: false
```

---

### 11.3 Cài runc

```bash
sudo apt install -y runc
mydocker runtime   # kiểm tra lại
```

---

### 11.4 Chạy container bằng runc trực tiếp

```bash
# Tạo OCI bundle với rootfs từ image
sudo mkdir -p /tmp/runc-test/rootfs
sudo cp -r /var/lib/mydocker/images/alpine_latest/layers/*/. /tmp/runc-test/rootfs/ 2>/dev/null || \
  sudo cp -r /var/lib/mydocker/images/alpine_latest/layers/$(ls /var/lib/mydocker/images/alpine_latest/layers/ | head -1)/. /tmp/runc-test/rootfs/

# Generate spec chuẩn
sudo runc spec --bundle /tmp/runc-test

# Sửa command
sudo python3 -c "
import json
with open('/tmp/runc-test/config.json') as f: cfg = json.load(f)
cfg['process']['args'] = ['/bin/sh', '-c', 'echo Hello from runc!']
cfg['process']['terminal'] = False
with open('/tmp/runc-test/config.json', 'w') as f: json.dump(cfg, f, indent=2)
"

# Chạy bằng runc
sudo runc run --bundle /tmp/runc-test my-runc-container
```

Output:
```
Hello from runc!
```

Điều này chứng minh rootfs và image format của mydocker **compatible với runc production runtime**.

---

## 12. Ví dụ thực tế end-to-end

### Ví dụ 1 — Static website với custom HTML

```bash
mkdir -p ~/website/html
cat > ~/website/html/index.html << 'EOF'
<!DOCTYPE html>
<html>
<body>
  <h1>My Website</h1>
  <p>Served by mydocker + nginx</p>
</body>
</html>
EOF

mydocker run -d \
  --net \
  --name website \
  -p 8080:80 \
  -v ~/website/html:/usr/share/nginx/html \
  nginx:latest

sleep 2
IP=$(mydocker ps | grep website | awk '{print $7}')
curl http://$IP
```

---

### Ví dụ 2 — Build và chạy app Go

```bash
mkdir -p ~/goapp && cd ~/goapp

cat > main.go << 'EOF'
package main

import (
    "fmt"
    "net/http"
    "os"
)

func main() {
    port := os.Getenv("PORT")
    if port == "" { port = "8080" }
    http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprintf(w, "Hello from mydocker! Port: %s\n", port)
    })
    fmt.Printf("Listening on :%s\n", port)
    http.ListenAndServe(":"+port, nil)
}
EOF

cat > Dockerfile << 'EOF'
FROM alpine:latest
RUN apk add --no-cache go
WORKDIR /app
COPY main.go .
RUN go build -o server main.go
ENV PORT=9000
EXPOSE 9000
CMD ["/app/server"]
EOF

mydocker build -t goapp:v1 .
mydocker run -d --net --name goapp -e PORT=9000 goapp:v1
sleep 2

IP=$(mydocker ps | grep goapp | awk '{print $7}')
curl http://$IP:9000
```

---

### Ví dụ 3 — Multi-container với compose

```bash
mkdir -p ~/stack && cd ~/stack

cat > docker-compose.yml << 'EOF'
version: "3"

services:
  frontend:
    image: nginx:latest
    ports:
      - "8080:80"
    volumes:
      - ./html:/usr/share/nginx/html
    restart: always

  backend:
    image: alpine:latest
    command: /bin/sh -c "while true; do echo '[backend] processing...'; sleep 5; done"
    environment:
      - NODE_ENV=production
      - API_KEY=secret123
    depends_on:
      - frontend

  logger:
    image: alpine:latest
    command: /bin/sh -c "while true; do date >> /logs/app.log; sleep 10; done"
    volumes:
      - ./logs:/logs
EOF

mkdir -p html logs
echo "<h1>Hello Stack!</h1>" > html/index.html

sudo mydocker-compose up -d
sleep 3

echo "=== Services ==="
sudo mydocker-compose ps

echo "=== Frontend logs ==="
sudo mydocker-compose logs frontend | tail -5

echo "=== Backend logs ==="
sudo mydocker-compose logs backend | tail -5

echo "=== Logger output ==="
cat logs/app.log

sudo mydocker-compose down
```

---

### Ví dụ 4 — Resource limits + monitoring

```bash
# Chạy container với giới hạn tài nguyên
mydocker run -d \
  --name monitored \
  --memory=64m \
  --cpus=0.5 \
  --pids=30 \
  alpine:latest \
  /bin/sh -c "while true; do echo working; sleep 2; done"

sleep 2

# Monitor resource usage
mydocker stats monitored

# Cleanup
mydocker stop monitored
mydocker rm monitored
```

---

## Tóm tắt flags

### `mydocker run`

| Flag | Ví dụ | Mô tả |
|------|-------|-------|
| `-it` | `run -it alpine` | Interactive + TTY |
| `-d` | `run -d nginx` | Detach (background) |
| `--name` | `--name web` | Đặt tên container |
| `--hostname` | `--hostname myhost` | Hostname bên trong container |
| `-e` | `-e KEY=val` | Environment variable |
| `-p` | `-p 8080:80` | Port forwarding |
| `-v` | `-v ./data:/data` | Volume mount |
| `-v` (ro) | `-v ./cfg:/cfg:ro` | Read-only volume |
| `-w` | `-w /app` | Working directory |
| `--net` | `--net` | Bật networking |
| `--no-net` | `--no-net` | Tắt networking |
| `--memory` | `--memory=256m` | Giới hạn RAM |
| `--cpus` | `--cpus=0.5` | Giới hạn CPU |
| `--pids` | `--pids=50` | Giới hạn processes |
| `--restart` | `--restart=always` | Restart policy |
| `--entrypoint` | `--entrypoint /bin/sh` | Override entrypoint |

### `mydocker build`

| Flag | Ví dụ | Mô tả |
|------|-------|-------|
| `-t` | `-t myapp:v1` | Tag cho image |
| `-f` | `-f Dockerfile.prod` | Custom Dockerfile path |
| `--no-cache` | `--no-cache` | Bỏ qua cache |

### `mydocker-compose`

| Lệnh | Mô tả |
|------|-------|
| `up` | Khởi động tất cả services |
| `up -d` | Khởi động background |
| `down` | Dừng và xóa tất cả |
| `ps` | Xem trạng thái |
| `logs [service]` | Xem logs |
| `exec <svc> <cmd>` | Exec vào service |
| `-f <file>` | Dùng file compose khác |
