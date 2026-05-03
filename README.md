# mydocker

Container runtime viết bằng Go, chạy trên WSL2 Ubuntu 22.04.

Không phải wrapper của Docker — project này implement lại trực tiếp các kernel primitives: Linux namespaces, cgroups v2, overlayfs, và pivot_root.

---

## Tại sao làm cái này

Docker là black box với hầu hết lập trình viên. Mục tiêu ở đây là hiểu thực sự điều gì xảy ra khi chạy `docker run` — không phải ở tầng API, mà ở tầng kernel. Syscall nào được gọi, filesystem được cô lập như thế nào, resource limits hoạt động ra sao, network được kết nối như thế nào.

Cách tốt nhất để hiểu một thứ là tự xây dựng lại nó.

Mục tiêu ban đầu chỉ là hiểu sâu bốn concepts:

1. **Namespaces** — cách kernel cho process thấy view riêng của PID, mount, network, UTS, IPC, user
2. **Cgroups v2** — cách kernel giới hạn tài nguyên (CPU, memory, I/O) cho một group process
3. **Overlayfs** — union filesystem để compose container rootfs từ image layers
4. **Capabilities** — fine-grained permission system thay thế binary root/non-root

Nhưng đã phát triển thêm nhiều hơn thế.

---

## Làm được gì

```bash
mydocker pull nginx:latest
mydocker build -t myapp:v1 .
mydocker run -d --net -p 8080:80 --name web nginx:latest
mydocker run -it --memory=256m -v ./data:/data alpine:latest
mydocker exec web /bin/sh
mydocker logs web
mydockerd &                        # daemon với restart policy
mydocker run -d --restart=always --name web nginx:latest
mydocker-compose up -d             # tương thích docker-compose
```

## Hoạt động như thế nào

| Tính năng | Kernel primitive |
|-----------|-----------------|
| Process isolation | PID, UTS, IPC, Mount namespaces |
| Filesystem isolation | overlayfs + pivot_root |
| Resource limits | cgroup v2 |
| Networking | veth pair + bridge + iptables NAT |
| Image layers | Docker Registry API v2 |
| OCI compatibility | Runtime Spec v1.0.2 (tương thích runc) |

---

## Tài liệu

Xem [`document/`](./docs) để biết cách cài đặt, sử dụng, kiến trúc, và cơ chế bên trong.
