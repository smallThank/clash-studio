# Clash 配置台

通过网页管理多份 OpenClash 和 Clash Verge Rev 配置，编辑节点、代理组、DNS 和分流规则，下载 YAML 或发布可更新的订阅。

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
