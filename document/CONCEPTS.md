# Các khái niệm cơ bản — Docker và Container

Tài liệu này dành cho người **chưa từng dùng Docker**, giải thích từ vấn đề thực tế đến cách container giải quyết nó, và mydocker liên quan như thế nào.

---

## 1. Vấn đề — "Works on my machine"

Hãy tưởng tượng bạn viết một web app trên máy tính cá nhân. App chạy tốt. Bạn gửi code cho bạn cùng nhóm — app báo lỗi. Bạn deploy lên server — app không chạy.

Lý do thường gặp:

- Máy bạn dùng Python 3.11, server dùng Python 3.8
- Bạn cài thư viện `libpq` version 14, bạn cùng nhóm chưa cài
- Biến môi trường trên máy bạn khác server
- Hệ điều hành khác nhau (Ubuntu vs CentOS vs macOS)

Đây là vấn đề tồn tại hàng chục năm trong nghề lập trình. Giải pháp truyền thống là viết tài liệu "cài đặt môi trường" — nhưng tài liệu nhanh chóng lỗi thời và thiếu sót.

---

## 2. Giải pháp đầu tiên — Virtual Machine

Nếu môi trường khác nhau gây vấn đề, tại sao không **đóng gói cả môi trường** vào một thứ duy nhất?

Virtual Machine (VM) làm đúng điều đó: tạo ra một máy tính ảo hoàn chỉnh, có OS riêng, ổ cứng riêng, RAM riêng.

```
┌─────────────────────────────────────┐
│           Physical Machine          │
│                                     │
│  ┌─────────┐      ┌─────────┐       │
│  │  VM 1   │      │  VM 2   │       │
│  │ Ubuntu  │      │ CentOS  │       │
│  │ Python  │      │ Node.js │       │
│  │ App A   │      │ App B   │       │
│  └─────────┘      └─────────┘       │
│                                     │
│         Hypervisor (VMware...)      │
│         Host OS (Windows/Linux)     │
└─────────────────────────────────────┘
```

**Vấn đề của VM:**
- Nặng: mỗi VM tốn 1–10 GB disk, 512MB–2GB RAM chỉ để chạy OS
- Khởi động chậm: 30 giây đến vài phút
- Lãng phí: 10 VM = 10 bản copy của kernel Linux

---

## 3. Giải pháp tốt hơn — Container

Container giải quyết vấn đề tương tự VM nhưng **không tạo ra máy ảo**. Thay vào đó, container **dùng chung kernel** của host, chỉ cô lập phần userspace (ứng dụng và thư viện).

```
┌─────────────────────────────────────────┐
│              Physical Machine           │
│                                         │
│  ┌───────────┐  ┌───────────┐           │
│  │Container 1│  │Container 2│           │
│  │ Python App│  │ Node App  │           │
│  │ libs      │  │ libs      │           │
│  └───────────┘  └───────────┘           │
│                                         │
│          Host OS + Kernel               │
│     (dùng chung, không copy)            │
└─────────────────────────────────────────┘
```

**So sánh:**

| | VM | Container |
|--|----|----|
| Khởi động | 30s – 2 phút | < 1 giây |
| Dung lượng | 1–10 GB | 5–200 MB |
| RAM overhead | 512MB+ | ~10MB |
| Cô lập | Hoàn toàn (khác kernel) | Gần hoàn toàn (chung kernel) |
| Bảo mật | Cao hơn | Thấp hơn một chút |

Container không thay thế hoàn toàn VM — trong môi trường đòi hỏi bảo mật tối đa (ngân hàng, quân sự) người ta vẫn dùng VM. Nhưng cho 95% use case của lập trình viên, container đủ tốt và nhanh hơn nhiều.

---

## 4. Image là gì?

Image là **bản thiết kế** (blueprint) của container — một file chứa toàn bộ filesystem cần thiết để chạy ứng dụng: OS files, thư viện, code, config.

Hãy nghĩ image như một **USB boot** — bạn có thể tạo ra nhiều máy tính giống nhau từ cùng một USB.

```
Image alpine:latest
├── /bin/sh
├── /bin/ls
├── /etc/
│   └── os-release  →  "Alpine Linux v3.23"
├── /lib/
└── /usr/
```

Image **không chứa kernel** — nó chỉ chứa userspace. Kernel của host được dùng chung.

### Image đến từ đâu?

Docker Hub (`hub.docker.com`) là kho lưu trữ image công khai, giống như GitHub cho image. Ai cũng có thể upload image lên đó và download về.

```bash
mydocker pull alpine:latest   # tải từ Docker Hub
mydocker pull nginx:latest
```

---

## 5. Container là gì?

Container là **một instance đang chạy** của image — giống như process là instance đang chạy của executable.

```
Image alpine:latest  →  Container A (đang chạy /bin/sh)
                     →  Container B (đang chạy /bin/sh)
                     →  Container C (đang chạy /bin/sh)
```

Ba containers chạy từ cùng một image, nhưng **hoàn toàn độc lập** — process trong A không thấy process trong B, file A tạo ra không ảnh hưởng B.

Khi container bị xóa, mọi thay đổi bên trong mất đi — image gốc không bị ảnh hưởng. Đây là tính chất **ephemeral** (tạm thời) của container.

---

## 6. Image Layer là gì?

Image không phải một file đơn khổng lồ — nó được tổ chức thành nhiều **layer** (tầng), mỗi layer là một tập hợp thay đổi so với layer trước.

```
Layer 3: COPY app.py /app/         ← 50 KB
Layer 2: RUN pip install flask     ← 15 MB
Layer 1: FROM python:3.11          ← 50 MB (base OS + Python)
```

**Tại sao dùng layer?**

Nếu bạn có 10 images đều dùng `python:3.11` làm base, thay vì lưu 10 bản copy 50MB, hệ thống chỉ lưu 1 bản layer 50MB và tái sử dụng cho tất cả.

```
Image A:  [python:3.11 layer] + [layer flask] + [layer app_a]
Image B:  [python:3.11 layer] + [layer django] + [layer app_b]
           ↑ dùng chung, chỉ lưu 1 lần
```

---

## 7. Dockerfile là gì?

Dockerfile là **công thức** để tạo image — một file text mô tả từng bước xây dựng image.

```dockerfile
FROM python:3.11          # bắt đầu từ image có sẵn
RUN pip install flask     # chạy lệnh trong quá trình build
COPY app.py /app/         # copy file từ host vào image
WORKDIR /app              # set thư mục làm việc
CMD ["python", "app.py"]  # lệnh mặc định khi run container
```

Mỗi instruction tạo ra một layer mới. Build image:

```bash
mydocker build -t myapp:v1 .
```

---

## 8. Container cô lập như thế nào?

Container cô lập nhờ **Linux kernel features** — không phải phần mềm giả lập. Kernel Linux có sẵn cơ chế này từ nhiều năm trước khi Docker ra đời.

### Namespace — cô lập "cái nhìn"

Namespace cho phép một process **thấy một view riêng** của hệ thống, khác với những process khác.

**Ví dụ PID namespace:**

```
Host thấy:                Container thấy:
PID 1   systemd           PID 1   /bin/sh   ← đây là process của container
PID 234 sshd                               (không thấy systemd, sshd...)
PID 891 mydocker
PID 892 /bin/sh  ← đây là container process trên host
```

Cùng một process, nhưng trên host nó là PID 892, trong container nó thấy mình là PID 1.

**Các loại namespace mydocker dùng:**

| Namespace | Cô lập cái gì | Ví dụ |
|-----------|--------------|-------|
| PID | Danh sách process | Container chỉ thấy process của nó |
| Mount | Filesystem | Container có filesystem riêng |
| UTS | Hostname | Container có hostname riêng |
| Network | Network interfaces | Container có IP riêng |
| IPC | Inter-process communication | Shared memory, semaphores |
| User | UID/GID | Root trong container ≠ root trên host |

### cgroup — giới hạn tài nguyên

Namespace cô lập "cái nhìn" nhưng không giới hạn tài nguyên. Nếu không có giới hạn, một container có thể dùng hết 100% CPU hay RAM của host.

**cgroup (control groups)** cho phép kernel giới hạn tài nguyên cho một nhóm process:

```bash
mydocker run --memory=100m --cpus=0.5 alpine:latest
# Container này tối đa dùng 100MB RAM và 50% 1 CPU
```

Đây là cơ chế kernel thật — không phải phần mềm giám sát sau thực tế.

---

## 9. Container khác VM ở điểm cốt lõi nào?

| | VM | Container |
|--|----|----|
| Kernel | Mỗi VM có kernel riêng | Dùng chung kernel host |
| Cô lập bằng | Hypervisor (phần cứng ảo) | Linux namespaces (kernel feature) |
| Filesystem | Disk ảo riêng biệt | overlayfs trên host filesystem |
| Bảo mật | Escape = phải break hypervisor | Escape = exploit kernel namespace |

**Hệ quả quan trọng:** Docker Engine **không chạy native trên Windows/macOS** vì kernel Linux không có trên đó. Docker Desktop cài một VM Linux nhỏ bên dưới rồi chạy container trong VM đó. mydocker chạy trong WSL2 vì WSL2 chính là một VM Linux chạy trên Windows.

---

## 10. Docker thật và mydocker khác nhau như thế nào?

Docker là một ecosystem lớn gồm nhiều thành phần:

```
Docker CLI  →  Docker Engine (dockerd)  →  containerd  →  runc
```

mydocker implement lại toàn bộ pipeline này từ đầu, dùng Go, nhưng đơn giản hơn và không có một số tính năng production như:

| Tính năng | Docker | mydocker |
|-----------|--------|----------|
| Container runtime | runc (qua containerd) | native + runc compatible |
| Image pull | Docker Registry v2 | Docker Registry v2 ✓ |
| Networking | CNI plugins | bridge + veth ✓ |
| Volumes | Volume drivers | bind mount ✓ |
| Compose | docker-compose | mydocker-compose ✓ |
| Swarm/K8s | Có | Không |
| Image build | BuildKit | basic Dockerfile ✓ |
| Registry push | Có | Không |
| Windows/macOS native | Có (qua VM) | Không |

**Mục đích của mydocker** để **hiểu Docker hoạt động như thế nào** bằng cách xây dựng lại từ đầu.

---

## 11. Tóm tắt

```
Vấn đề:
  "Works on my machine" → môi trường khác nhau → app lỗi

Giải pháp VM:
  Đóng gói cả OS → nặng, chậm

Giải pháp Container:
  Dùng chung kernel, cô lập userspace → nhẹ, nhanh

Container hoạt động nhờ:
  Namespace    → cô lập "cái nhìn" của process
  cgroup       → giới hạn tài nguyên
  overlayfs    → filesystem layer hiệu quả
  pivot_root   → thay đổi root filesystem

Image:
  Blueprint của container, lưu dưới dạng layers

Dockerfile:
  Công thức để tạo image

Docker Hub:
  Kho lưu trữ image công khai
```
