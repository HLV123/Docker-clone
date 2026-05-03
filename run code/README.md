# mydocker — Demo Run

Thư mục này chứa toàn bộ source code sau khi đã chạy end-to-end tất cả tính năng.

---

## Cấu trúc

```
run code/
└── mydocker/
    ├── cmd/                    ← source code 3 binaries
    ├── internal/               ← toàn bộ logic
    ├── scripts/                ← setup scripts
    ├── example/                ← docker-compose.yml mẫu
    ├── Makefile
    ├── go.mod / go.sum
    └── runtime-snapshot/       ← DẤU VẾT SAU KHI CHẠY
        ├── images/             ← metadata của images đã pull + build
        ├── containers/         ← state của tất cả containers đã chạy
        └── dns/hosts           ← DNS entries giữa các containers
```

---

## runtime-snapshot/ — Đọc cái này

Đây là snapshot của `/var/lib/mydocker/` sau khi chạy end-to-end tất cả tính năng. Không phải mock, không phải tạo tay — sinh ra tự nhiên khi chạy.

### Images đã pull và build

```
runtime-snapshot/images/
├── alpine_latest/              ← mydocker pull alpine:latest
│   ├── manifest.json           ← Docker Registry manifest v2
│   └── config.json             ← Entrypoint, Cmd, Env, WorkingDir
├── alpine_3.18/
│   └── manifest.json
├── nginx_latest/               ← mydocker pull nginx:latest (7 layers)
│   ├── manifest.json
│   └── config.json
├── demo-app_v1/                ← mydocker build -t demo-app:v1 .
│   └── config.json             ← image tự build từ Dockerfile
└── myapp_v1/
    └── config.json
```

### Containers đã chạy

Mỗi thư mục trong `containers/` là một container với:

- `state.json` — ID, image, command, PID, status, IP, ports, timestamps
- `config.json` — volumes, env vars, hostname, working directory
- `container.log` — stdout/stderr khi chạy detach mode (`-d`)
- `restart_policy` — nếu container có `--restart=always`

Các containers có trong snapshot:

| Name | Image | Tính năng test |
|------|-------|----------------|
| `webserver` | nginx:latest | `--net -p 8080:80` port forwarding |
| `api-service` | alpine:latest | `--net` container networking |
| `limited-app` | alpine:latest | `--memory=64m --cpus=0.5` resource limits |
| `env-demo` | alpine:latest | `-e DB_URL=...` environment variables |
| `demo-built` | demo-app:v1 | image tự build từ Dockerfile |
| `auto-restart` | alpine:latest | `--restart=always` daemon restart policy |
| `example_web` | nginx:latest | mydocker-compose up |
| `example_api` | alpine:latest | mydocker-compose up |
| `example_db` | alpine:latest | mydocker-compose up |

### DNS entries

```
runtime-snapshot/dns/hosts
```

File này được inject vào `/etc/hosts` của mỗi container khi start — cho phép containers gọi nhau bằng tên thay vì IP.

### OCI bundle

Một số containers có thêm `bundle/config.json` — đây là OCI Runtime Spec v1.0.2, compatible với `runc`.

---

## Những gì đã chạy để tạo ra snapshot này

```bash
# Pull images từ Docker Hub
mydocker pull alpine:latest
mydocker pull nginx:latest

# Build image từ Dockerfile
mydocker build -t demo-app:v1 /tmp/demo-app

# Run containers với các tính năng khác nhau
mydocker run -d --net --name webserver -p 8080:80 nginx:latest
mydocker run -d --net --name api-service alpine:latest /bin/sh -c "sleep infinity"
mydocker run -d --memory=64m --cpus=0.5 --name limited-app alpine:latest /bin/sh -c "sleep infinity"
mydocker run -d -e DB_URL=postgres://localhost/mydb --name env-demo alpine:latest /bin/sh -c "sleep infinity"
mydocker run -d --name demo-built demo-app:v1 /bin/sh -c "sleep infinity"

# Daemon + restart policy
mydockerd &
mydocker run -d --restart=always --name auto-restart alpine:latest /bin/sh -c "sleep 3"

# docker-compose
mydocker-compose up -d   # start web + api + db
mydocker-compose down
```
