# Hướng dẫn cài đặt mydocker

Tài liệu này hướng dẫn từng bước để cài đặt và chạy **mydocker** trên Windows với WSL2.  
Làm theo đúng thứ tự, mỗi bước hoàn thành mới sang bước tiếp.

---

## Yêu cầu

- Windows 10 (build 19041+) hoặc Windows 11
- Kết nối internet
- Quyền Administrator trên Windows

---

## Bước 1 — Cài WSL2

Mở **PowerShell với quyền Administrator** (click chuột phải vào Start → Windows PowerShell (Admin)).

```powershell
wsl --install -d Ubuntu-22.04
```

Lệnh này sẽ tải và cài Ubuntu 22.04. Quá trình mất 5–10 phút.

Sau khi cài xong, **khởi động lại máy tính**.

---

## Bước 2 — Tạo tài khoản Ubuntu

Sau khi reboot, Ubuntu sẽ tự mở và hỏi:

```
Enter new UNIX username: hung
New password:
Retype new password:
```

Nhập username và password (khi nhập password màn hình không hiện ký tự — bình thường).

Kiểm tra WSL2 đang chạy đúng version, mở lại PowerShell:

```powershell
wsl --list --verbose
```

Kết quả mong muốn:
```
  NAME            STATE           VERSION
* Ubuntu-22.04    Running         2
```

Cột VERSION phải là **2**. Nếu là 1, chạy thêm:

```powershell
wsl --set-version Ubuntu-22.04 2
```

---

## Bước 3 — Cấu hình WSL2

Mở Ubuntu (tìm "Ubuntu 22.04" trong Start Menu).

Tạo file cấu hình WSL2:

```bash
sudo nano /etc/wsl.conf
```

Dán nội dung sau vào (thay `hung` bằng username của bạn):

```ini
[boot]
systemd=true

[user]
default=hung
```

Lưu file: nhấn `Ctrl+O` → `Enter` → `Ctrl+X`.

Tắt WSL từ PowerShell để áp dụng cấu hình:

```powershell
wsl --shutdown
```

Mở lại Ubuntu và kiểm tra systemd hoạt động:

```bash
systemctl --version
```

Kết quả mong muốn: hiện ra số version (ví dụ `systemd 249`).

---

## Bước 4 — Cài Go

Trong Ubuntu terminal:

```bash
mkdir -p ~/go-install
wget https://go.dev/dl/go1.23.0.linux-amd64.tar.gz
tar -xzf go1.23.0.linux-amd64.tar.gz -C ~/go-install
echo 'export PATH=$PATH:~/go-install/go/bin' >> ~/.bashrc
source ~/.bashrc
```

Kiểm tra:

```bash
go version
```

Kết quả mong muốn:
```
go version go1.23.0 linux/amd64
```

---

## Bước 5 — Cài dependencies hệ thống

```bash
sudo apt update
sudo apt install -y build-essential git iproute2 iptables \
    ca-certificates uidmap runc unzip zip
```

Quá trình này mất 1–3 phút.

---

## Bước 6 — Tải source code

### Cách A: Clone từ repo

### Cách B: Giải nén từ file zip

Copy file `mydocker.zip` vào máy (ví dụ để ở `C:\Users\ADMIN\Downloads\`), sau đó trong Ubuntu:

```bash
cp /mnt/c/Users/ADMIN/Downloads/mydocker.zip ~/
cd ~
unzip mydocker.zip
cd mydocker
```

---

## Bước 7 — Build

```bash
cd ~/mydocker
make build
```

Kết quả mong muốn:
```
Building mydocker...
Build OK: mydocker, mydockerd, mydocker-compose
```

Sẽ tạo ra 3 file binary: `mydocker`, `mydockerd`, `mydocker-compose`.

---

## Bước 8 — Install

```bash
sudo make install
```

Lệnh này copy binary vào `/usr/local/bin/` và tạo wrapper script để tự động dùng `sudo` khi cần.

Kiểm tra:

```bash
which mydocker
mydocker --help 2>/dev/null | head -5 || mydocker 2>&1 | head -5
```

---

## Bước 9 — Setup hệ thống

```bash
sudo bash scripts/setup.sh
```

Script này tự động:
- Bật cgroup v2 controllers
- Tạo network bridge `mydocker0`
- Cấu hình iptables NAT
- Tạo thư mục runtime `/var/lib/mydocker/`
- Cấu hình systemd cgroup delegation

Kết quả mong muốn — tất cả dòng đều hiện `[OK]`:
```
[OK] Dependencies installed
[OK] cgroup v2 unified hierarchy
[OK] cgroup controllers enabled
[OK] overlayfs available
[OK] Network bridge configured
[OK] Runtime dirs: /var/lib/mydocker/
[OK] /etc/wsl.conf already configured
[OK] Network config will persist across reboots
```

---

## Bước 10 — Verify môi trường

Chạy script kiểm tra:

```bash
bash -c '
echo "=== Kernel ===" && uname -r
echo "=== Cgroup v2 ===" && mount | grep -q "cgroup2" && echo OK || echo FAIL
echo "=== Overlayfs ===" && grep -q overlay /proc/filesystems && echo OK || echo FAIL
echo "=== Bridge ===" && ip link show mydocker0 && echo OK || echo FAIL
echo "=== mydocker ===" && mydocker 2>&1 | head -2
'
```

---

## Bước 11 — Test cơ bản

### Pull image đầu tiên

```bash
mydocker pull alpine:latest
```

Kết quả:
```
Pulling alpine:latest from Docker Hub...
Successfully pulled alpine:latest
```

### Chạy container

```bash
mydocker run -it alpine:latest
```

Sẽ vào shell bên trong container Alpine. Thử:

```sh
hostname
cat /etc/os-release
ps aux
exit
```

### Xem danh sách containers

```bash
mydocker ps -a
```

### Xem danh sách images

```bash
mydocker images
```

---

## Bước 12 — Test nâng cao

### Chạy nginx với port forwarding

```bash
mydocker pull nginx:latest
mydocker run -d --net -p 8080:80 --name webserver nginx:latest
sleep 2
mydocker ps
```

Lấy IP của container:

```bash
IP=$(mydocker ps | grep webserver | awk '{print $7}')
curl http://$IP | head -3
```

Kết quả mong muốn:
```html
<!DOCTYPE html>
<html>
<head>
```

### Dọn dẹp

```bash
mydocker stop webserver
mydocker rm webserver
```

---

## Bước 13 — Chạy daemon (tuỳ chọn)

Daemon cho phép dùng `--restart=always` và quản lý container background.

Mở terminal thứ nhất, chạy daemon:

```bash
sudo mydockerd
```

Mở terminal thứ hai, dùng bình thường:

```bash
mydocker run -d --restart=always --name web nginx:latest
mydocker ps
mydocker logs web
```

---

## Bước 14 — docker-compose (tuỳ chọn)

```bash
cd ~/mydocker/example
cat docker-compose.yml      # xem cấu hình mẫu
sudo mydocker-compose up -d
sudo mydocker-compose ps
sudo mydocker-compose logs web
sudo mydocker-compose down
```

---

## Bước 15 — Build image từ Dockerfile

Tạo thư mục project:

```bash
mkdir -p ~/myapp
cat > ~/myapp/Dockerfile << 'EOF'
FROM alpine:latest
RUN echo "hello" > /hello.txt
ENV APP_ENV=production
CMD ["/bin/sh", "-c", "cat /hello.txt && echo ENV=$APP_ENV"]
EOF

cd ~/myapp
mydocker build -t myapp:v1 .
mydocker run myapp:v1
```

Kết quả:
```
hello
ENV=production
```

---


## Tham khảo nhanh

| Lệnh | Mô tả |
|------|-------|
| `mydocker pull <image>` | Tải image từ Docker Hub |
| `mydocker images` | Danh sách images |
| `mydocker run -it <image>` | Chạy container interactive |
| `mydocker run -d <image>` | Chạy container background |
| `mydocker run -p 8080:80 <image>` | Port forwarding |
| `mydocker run -v /host:/container <image>` | Volume mount |
| `mydocker run -e KEY=val <image>` | Environment variable |
| `mydocker run --memory=100m <image>` | Giới hạn memory |
| `mydocker ps` | Containers đang chạy |
| `mydocker ps -a` | Tất cả containers |
| `mydocker stop <id>` | Dừng container (graceful) |
| `mydocker rm <id>` | Xóa container |
| `mydocker logs <id>` | Xem logs |
| `mydocker exec <id> /bin/sh` | Vào container đang chạy |
| `mydocker stats <id>` | CPU/memory usage |
| `mydocker inspect <id>` | Chi tiết container/image |
| `mydocker build -t <name> .` | Build image từ Dockerfile |
| `mydocker spec <id>` | Xem OCI config.json |
| `mydockerd` | Khởi động daemon |
| `mydocker-compose up -d` | Chạy docker-compose |
| `mydocker-compose down` | Dừng docker-compose |
