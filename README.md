# Clash 配置台

通过网页管理多份 OpenClash 和 Clash Verge Rev 配置，编辑节点、代理组、DNS 和分流规则，下载 YAML 或发布可更新的订阅。

## Docker 部署

在项目根目录构建并启动镜像。一个容器同时提供前端、管理 API 和订阅接口，容器内统一使用 8080 端口。

```sh
docker build -t clash-studio:local .
docker run -d --name clash-studio --restart unless-stopped \
  -p 127.0.0.1:8080:8080 \
  --mount type=volume,source=clash-studio-data,target=/data \
  clash-studio:local
```

浏览器打开 <http://127.0.0.1:8080>。可访问 <http://127.0.0.1:8080/healthz> 检查服务。

- **跨设备订阅**。页面生成的订阅链接默认使用浏览器当前访问后台的协议、域名或 IP，以及端口。使用宿主机 HTTPS 反向代理时，可保留上面的本机端口绑定，并让反代保留原始 `Host`。直接对外提供 HTTP 时，可用 `-p 8080:8080`。如需统一使用固定的公开地址，再设置 `PUBLIC_BASE_URL`。
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

## 使用上游节点订阅

- 在「订阅源 → 添加订阅源」填写原始订阅地址。导出的 YAML 通过 `proxy-providers` 保留地址，代理组在「订阅源」中引用它，由客户端按更新间隔拉取节点。
- 在「订阅源」或「代理节点」中选择「导入订阅节点」，填写原始订阅地址，预览后选择「导入到草稿」。应用从 Clash/Mihomo YAML 提取 `proxies`，追加到当前配置。遇到同名节点时保留已有节点。代理组在「显式成员」中选择这些节点。导出和发布使用导入时保存的节点，后续可按需再次导入新节点。

## 设置代理组的默认节点

编辑「手动选择」代理组，在「默认节点」选择显式成员。要使用订阅节点，先勾选该组的订阅源，首次点击「读取订阅节点」并选中目标。成功读取的节点名单保存在服务端，再次编辑时直接显示。需要同步上游变化时，点击「刷新节点名单」。也可以直接填写客户端显示的完整节点名。

读取名单时，后台优先使用订阅源的 `header.User-Agent`，其次是配置的 `global-ua`，默认发送 `clash.meta/1.19.34`。如果服务商要求特定标识，可在订阅源的「其他字段（YAML）」配置 `header.User-Agent` 并重试。若上游返回的是通过 `proxy-providers` 引用节点的完整配置，请使用其中对应的节点订阅地址读取名单。

保存后会写入 `default-selected`，由 Mihomo v1.19.28 及更新版本读取。旧内核可把目标节点移到「显式成员」的第一位。客户端保存的历史选择可能优先。 [Mihomo 字段说明](https://wiki.metacubex.one/config/proxy-groups/#default-selected) · [v1.19.28 发布记录](https://github.com/MetaCubeX/mihomo/releases/tag/v1.19.28)

## 主题与订阅密钥

页面右上角可切换浅色和深色皮肤。首次访问跟随系统外观，手动选择后保存在当前浏览器。

每份配置在创建时生成独立密钥。打开「配置管理 → 密钥管理」可查看、复制或轮换。发布后，在「订阅链接 → 查看链接」复制带 `?key=` 的完整地址，直接粘贴到 OpenClash 或 Clash Verge Rev。程序也可以用同一接口地址和 `Authorization: Bearer` 请求头读取 YAML。

```sh
curl -fsS -H "Authorization: Bearer $CLASH_SUBSCRIPTION_KEY" \
  'https://clash.example.com/s/替换为实际链接ID.yaml'
```

重新发布会更新订阅内容。轮换密钥后，此配置的旧订阅地址立即失效，需要在客户端粘贴带新密钥的地址。升级旧版本时，数据库会为原有配置生成密钥，保留已发布的 YAML 和设备链接记录。请从页面重新复制订阅地址。

## 持久化与订阅访问

| 环境变量 | 用途 |
| --- | --- |
| `ADDR` | Go 服务监听地址，默认 `127.0.0.1:8080`。 |
| `PUBLIC_BASE_URL` | 可选的固定订阅公开地址。留空时，管理页面使用浏览器当前地址。监听地址 `0.0.0.0` 和 `[::]` 会按留空处理。 |
| `DATA_DIR` | SQLite 数据目录，默认为当前账户的系统应用配置目录下的 `clash-config-studio`。升级和备份时保留数据库及密钥。 |
| `CLASH_MASTER_KEY` | 可选的 32 字节 Base64 主密钥。留空时，应用在数据目录首次生成和使用 `master.key`。 |
| `FRONTEND_DIR` | 前端构建目录，默认 `frontend/dist`。 |

在 OpenClash 的「配置订阅」或 Clash Verge Rev 的远程订阅中粘贴发布链接。生成链接所指的网站需要能被客户端访问。客户端配置中的 HTTP 代理 Provider 和规则 Provider 仍由客户端访问各自的原地址。持有发布链接即可读取其中的代理配置，可在页面中逐条吊销。

检查或导入上游订阅时，如果容器 DNS 返回 OpenClash 常用的 Fake-IP 网段（`198.18.0.0/15`），应用会通过 Cloudflare 的加密 DNS 查询订阅域名，再连接通过公网地址检查的结果。DNS 查询只包含域名。若容器无法访问 `https://1.1.1.1/dns-query`，可在 OpenClash 的 Fake-IP 过滤列表加入订阅域名，让容器直接解析到公网地址。

网页检查会读取响应正文识别订阅格式。若收到网页，请在「请求 User-Agent」填入服务商要求的值重试。该值仅用于本次检查。如果服务商使用 AGE 加密节点源，可在「其他字段（YAML）」配置 `age-secret-key`，由支持 AGE 的 Mihomo 客户端拉取并解密。若服务商提供的地址返回整份配置，请从客户端导出解密后的 YAML，再通过「新建配置 → 导入 YAML」导入。 [Mihomo 节点源文档](https://wiki.metacubex.one/config/proxy-providers/#age-secret-key)

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
playwright-cli open http://127.0.0.1:8080
playwright-cli run-code --filename=tests/browser-layout.mjs
playwright-cli close
```

[需求分析](docs/requirements.md) · [技术设计](docs/technical-design.md) · [交互原型](prototype/README.md)
