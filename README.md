# Clash 配置台

通过网页管理多份 OpenClash 和 Clash Verge Rev 配置，编辑节点、代理组、DNS 和分流规则，下载 YAML 或发布可更新的订阅。

## Docker 部署

在项目根目录构建并启动镜像。一个容器同时提供前端、管理 API 和订阅接口，容器内统一使用 8080 端口。

```sh
docker build -t clash-studio:local .
docker run -d --name clash-studio --restart unless-stopped \
  -p 127.0.0.1:8080:8080 \
  -e PUBLIC_BASE_URL=http://127.0.0.1:8080 \
  --mount type=volume,source=clash-studio-data,target=/data \
  clash-studio:local
```

浏览器打开 <http://127.0.0.1:8080>。可访问 <http://127.0.0.1:8080/healthz> 检查服务。

- **跨设备订阅**。把 `PUBLIC_BASE_URL` 设为 OpenClash 和 Clash Verge Rev 在代理尚未启动时也能访问的地址。使用宿主机 HTTPS 反向代理时，保留上面的本机端口绑定，并将变量设为对外的 HTTPS 域名。若需要容器端口直接对外提供 HTTP，可用 `-p 8080:8080`，并将变量设为对应的访问地址。
- **数据保留**。命名卷 `clash-studio-data` 保存 SQLite 和自动生成的 `master.key`。更新镜像后重建容器时挂载同一卷。停容器备份时保留卷内数据库和密钥。若使用 `CLASH_MASTER_KEY` 注入密钥，另行保管同一个值。若改用宿主机目录挂载，请让该目录可由 UID/GID `10001:10001` 写入。
- **镜像架构**。直接在部署服务器构建可获得对应架构的镜像。从 Apple Silicon 将镜像搬到 x86-64 服务器时，构建命令使用 `docker buildx build --platform linux/amd64 --load -t clash-studio:local .`，再用 `docker save` 和 `docker load` 转移。

### 修复 `/data` 权限

镜像通过 `USER 10001:10001` 以固定的非 root 身份运行，前一个数字是 UID，后一个是 GID。绑定挂载会沿用宿主机目录的权限，应用需要以这个身份在 `/data` 写入 SQLite 数据库和主密钥。 [Dockerfile](Dockerfile)

日志出现 `open /data/.master-key-…: permission denied` 时，检查原容器的挂载。将下面的容器名换成实际名称或 ID。

```sh
container=clash-studio
docker inspect -f '{{range .Mounts}}{{if eq .Destination "/data"}}type={{.Type}} source={{.Source}} name={{.Name}} rw={{.RW}}{{end}}{{end}}' "$container"
```

若为 `type=bind` 且 `rw=true`，在宿主机以管理员身份修改 `source` 对应目录。下面的命令只修改数据目录和应用已存在的数据文件。

```sh
data_dir=/替换为上面显示的source
docker stop "$container" &&
  chown 10001:10001 "$data_dir" &&
  chmod u+rwx "$data_dir" &&
  find "$data_dir" -maxdepth 1 -type f \( -name master.key -o -name studio.sqlite -o -name studio.sqlite-wal -o -name studio.sqlite-shm \) -exec chown 10001:10001 {} + &&
  docker start "$container"
```

若为 `type=volume` 且 `rw=true`，用同一镜像临时修复卷内权限，再启动原容器。

```sh
volume=替换为上面显示的name
image=$(docker inspect -f '{{.Image}}' "$container")
docker stop "$container" &&
  docker run --rm --user 0:0 --entrypoint sh \
    --mount "type=volume,source=$volume,target=/data" "$image" -ec '
      chown 10001:10001 /data
      chmod u+rwx /data
      for file in /data/master.key /data/studio.sqlite /data/studio.sqlite-wal /data/studio.sqlite-shm; do
        if [ -f "$file" ]; then chown 10001:10001 "$file"; fi
      done
    ' &&
  docker start "$container"
```

如果显示 `rw=false`，使用原数据目录或原命名卷以可写方式重建容器。宿主机启用 SELinux 且目录权限修复后仍被拒绝时，专用于此容器的宿主机目录可用 `-v /实际数据目录:/data:Z` 重新挂载。重建时保留现有 SQLite 数据库和 `master.key`。

启动后运行 `docker exec "$container" curl -fsS http://127.0.0.1:8080/healthz`，看到 `ok` 表示服务已恢复。

## 本地运行

需要 Go 1.23+、支持 CGO 的 C 编译器，以及 Node.js 22.15+。

```sh
npm --prefix frontend ci
npm --prefix frontend run build
go run ./cmd/clash-studio
```

在浏览器打开 <http://127.0.0.1:8080>，注册后新建配置或导入已有 YAML。编辑保存为草稿，选择「下载 YAML」可以导出当前草稿。选择「发布订阅」后，可为每个设备生成一条订阅链接。再次发布时，已有链接会跟随最新的发布版本。

开发时可在另一终端运行 `npm --prefix frontend run dev`。Vite 默认将 API 和订阅请求转发到 `http://127.0.0.1:8080`，可通过 `CLASH_API_ORIGIN` 调整后端地址。启动 Go 服务时，可将开发页面的地址（例如 `http://127.0.0.1:5173`）设置为 `DEV_ORIGIN`。

## 持久化与订阅访问

| 环境变量 | 用途 |
| --- | --- |
| `ADDR` | Go 服务监听地址，默认 `127.0.0.1:8080`。 |
| `PUBLIC_BASE_URL` | 浏览器和代理客户端用于访问站点的公开地址，默认 `http://127.0.0.1:8080`。跨设备使用时填写两端均可访问的 HTTPS 地址，并在前置代理配置 TLS。 |
| `DATA_DIR` | SQLite 数据目录，默认为当前账户的系统应用配置目录下的 `clash-config-studio`。升级和备份时保留数据库及密钥。 |
| `CLASH_MASTER_KEY` | 可选的 32 字节 Base64 主密钥。留空时，应用在数据目录首次生成和使用 `master.key`。 |
| `FRONTEND_DIR` | 前端构建目录，默认 `frontend/dist`。 |

在 OpenClash 的「配置订阅」或 Clash Verge Rev 的远程订阅中粘贴发布链接。生成链接所指的网站需要能被客户端访问。客户端配置中的 HTTP 代理 Provider 和规则 Provider 仍由客户端访问各自的原地址。持有发布链接即可读取其中的代理配置，可在页面中逐条吊销。

## 测试

```sh
go test ./...
npm --prefix frontend test
npm --prefix frontend run build
```

可在运行 Go 测试时设置 `MIHOMO_BIN=/path/to/mihomo`，让整体测试调用指定 Mihomo 内核检查由应用导出的离线测试配置。

使用 Playwright CLI 复跑浏览器验收时，先用 `DATA_DIR="$(mktemp -d)" go run ./cmd/clash-studio` 启动独立的测试数据库，再在另一终端运行下列命令。测试会创建临时账号和配置。

```sh
playwright-cli open http://127.0.0.1:8080
playwright-cli run-code --filename=tests/browser-workflow.mjs
playwright-cli close
```

[需求分析](docs/requirements.md) · [技术设计](docs/technical-design.md) · [交互原型](prototype/README.md)
