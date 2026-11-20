# Clash 配置台需求分析

调研日期：2026-11-20　｜　状态：原型待确认

## 1. 目标与使用流程

用户在网页中管理多份代理配置，使用表单编辑订阅源、节点、代理组、DNS、扩展、规则集和路由规则。在同一页面检查引用、顺序和生成的 YAML，然后下载文件或发布订阅地址。OpenClash 和 Clash Verge Rev 按各自的更新设置获取已发布的配置，日常更新无需上传 Git。

1. 注册、登录，进入配置列表。新建空配置或导入现有 YAML，给路由器和桌面客户端分别维护配置。
2. 添加服务商的原始订阅地址。按实际返回内容选择作为整份配置导入，或作为客户端更新的代理节点源。
3. 先配置订阅源和节点，再组织代理组、规则集和路由规则。可单独调整 DNS 和扩展设置，随时保存草稿。
4. 查看生成的 YAML 和按模块定位的校验结果。通过后下载草稿对应的 YAML，或主动发布新版本。
5. 为每个客户端创建可撤销的订阅链接。再次发布后，原链接指向新的已发布版本，客户端在下次拉取时更新。

## 2. 两款客户端的兼容边界

| 组件 | 查阅结果 | 产品设计 |
| --- | --- | --- |
| OpenClash | OpenWrt 的 LuCI 插件。配置订阅支持原链接、可选在线转换、自定义 User-Agent 和自动更新，也支持本地上传配置。启动时可修改端口、DNS、TUN、规则及 Provider 路径，额外的覆写模块可继续修改运行 YAML。 | 提供可直接订阅的完整 YAML。对产品生成的链接，给出关闭在线订阅转换的导入说明。将客户端覆写开关作为联调变量。 [配置订阅与管理][oc-sub] · [覆写设置][oc-overwrite] · [运行架构][oc-arch] |
| Clash Verge Rev | 支持远程订阅和本地配置导入，内核基于 Mihomo。官方指南说明了应用设置、全局扩展与订阅扩展的处理顺序。自 v2.5.5 起，扩展中的 `dns`、`hosts` 整体替换，其他部分有各自的合并和回写规则。 | 支持粘贴订阅链接及导入 YAML 文件。发布前显示输出文档，联调时分别检查客户端扩展关闭和开启的结果。 [订阅导入][cv-profile] · [扩展配置][cv-extend] · [项目仓库][cv-repo] |
| Mihomo | 定义 YAML 的常规设置、DNS、出站协议、代理组、两类 Provider 和规则语法。规则由上至下匹配。 | 以两端实测的 Mihomo 版本为共同基线。同一份 YAML 可用于两端，涉及路由器监听方式和桌面 TUN 时，可复制为独立配置。 [配置总览][mh-config] · [路由规则][mh-rules] |

OpenClash 的 INI 风格覆写模块与 Clash Verge Rev 的扩展 YAML / JavaScript 分别由客户端处理。本应用的「扩展配置」第一期定义为 **发布前合入的 Mihomo 配置字段**，最终随完整 YAML 导出。OpenClash Smart 专属能力与两款客户端特有的覆写脚本进入后续评估。 [OpenClash 覆写模块][oc-module] · [Clash Verge Rev 扩展][cv-extend]

### 原始订阅地址有两种用法

| 用法 | 上游响应 | 结果与更新方式 |
| --- | --- | --- |
| 导入完整配置 | 含配置段的 Clash/Mihomo YAML | 导入预览后新建草稿。刷新需比较上游版本并确认，保留用户在网页中的编辑。 |
| 引用代理节点源 | 目标客户端可读的节点 Provider 内容，如包含 `proxies` 列表的 YAML | 地址原样写入 `proxy-providers.<name>.url`，代理组通过 `use` 选择。由客户端按 `interval` 更新节点。客户端需能访问原地址。 |

同一服务商 URL 可能按请求方式或 User-Agent 返回不同内容，首次识别需查看实际响应。第一期优先验收 YAML，Base64 或 URI 列表只在目标版本实测通过后标为兼容。遇到 HTML 登录页、识别不了的内容或需要第三方转换的地址，页面提示原因并引导提供 Clash/Mihomo YAML。 [OpenClash 订阅参数][oc-sub] · [Mihomo Provider][mh-pp]

## 3. 样例配置分析

本地只读分析 `/Users/taylorxie/Desktop/test.yaml`。解析了 15 个顶层键，未发现重复映射键。静态检查中，下表所列显式引用均能解析，节点与代理组的显式依赖未发现环。实际运行还取决于远程内容、目标版本和客户端覆写。下表仅记录结构与计数。

| 模块 | 样例结构 | 对界面的要求 |
| --- | --- | --- |
| 常规设置 | 9 个顶层标量，包括 HTTP、SOCKS、透明代理和混合端口，控制器地址、局域网访问、模式、日志、IPv6 | 常用开关、端口和地址有独立输入，保留显式的 `false`、`0` 和空值语义 |
| DNS | `enable`、`listen`、`enhanced-mode`、`fake-ip-range`、`use-hosts`；3 条默认解析器、2 条解析器、95 条 Fake-IP 过滤、7 条域名解析策略，其中 1 条的值为列表 | 解析器和值支持有序列表、批量粘贴；域名策略支持单值和多值；过滤表支持搜索 |
| 代理节点源 | 1 个 HTTP Provider，含 URL、路径、刷新间隔与 `health-check.enable/interval/url` | Provider 可命名，健康检查与刷新字段分开，地址默认遮盖凭据 |
| 手工节点 | 2 个 SOCKS5 节点，均含用户名、密码、`dialer-proxy` | SOCKS5 字段表单；可选现有策略组或节点作为拨号出口，检查循环 |
| 代理组 | 73 个组，58 个 `select`、11 个 `url-test`、4 个 `fallback`；69 个组使用 `use`，25 个组带 `filter` | 支持按名称、类型搜索；可选节点、组及 Provider；保留成员顺序、筛选表达式和测速参数 |
| 规则集 | 59 个 HTTP / `classical` Provider，带 `proxy`、URL、路径、更新间隔；未显式指定 `format` | 规则集引用及下载出口可选择；省略 `format` 时保留默认 YAML 的含义 |
| 路由规则 | 74 条，含 59 条 `RULE-SET`、4 条 `DOMAIN`、8 条 `DOMAIN-SUFFIX`、各 1 条 `GEOSITE`、`GEOIP`、`MATCH`；有 1 条 `RULE-SET` 含 `no-resolve`，`MATCH` 居末 | 表单编辑、批量导入、移动排序，展示规则集和目标引用；保留可选参数并识别兜底位置 |

## 4. 首期功能与字段

| 页面 | 首期提供的操作与配置 |
| --- | --- |
| 注册、登录、账户 | 用户名与密码注册、登录、登出。每个账户仅访问自己的配置、版本和订阅。 |
| 配置管理 | 新建、上传 YAML 导入、列表、切换、复制、改名、删除。保存草稿，查看已发布版本和更新时间。 |
| 订阅源 | 记录服务商原始地址及类型。整份 YAML 可导入预览；节点源映射为 HTTP Provider，支持 `interval`、`path`、健康检查、`filter/exclude-filter`；高级字段保留。 [代理 Provider][mh-pp] |
| 代理节点 | 新增、复制、删除和按协议编辑。SOCKS5 及 `dialer-proxy` 是样例验收项，表单计划覆盖 SS、VMess、VLESS、Trojan、Hysteria2。保留其余协议和传输参数的原始 YAML。 [出站协议][mh-proxy] · [SOCKS][mh-socks] · [代理链][mh-dialer] |
| 代理组 | `select`、`url-test`、`fallback`、`load-balance`；名称、显式成员 `proxies`、来源 `use`、筛选、测速 URL、间隔和容差。来自 Provider 的动态节点通过 `use` 选取。 [代理组][mh-group] |
| DNS | `enable/listen/ipv6/enhanced-mode/fake-ip-range`，`default-nameserver/nameserver/fallback`，`fake-ip-filter/nameserver-policy/proxy-server-nameserver/respect-rules`；余下字段通过高级 YAML 编辑。 [DNS][mh-dns] |
| 常规与扩展 | 常规页覆盖样例全部 9 项。扩展页可编辑 `tun`、`sniffer`、`hosts`、`profile` 等 Mihomo YAML，并预览生效键。客户端可能接管的字段给出提示。 [常规配置][mh-general] · [TUN][mh-tun] · [Sniffer][mh-sniff] |
| 规则集 | 配置 `http/file/inline`、`domain/ipcidr/classical`、`yaml/text/mrs`、`url/path/payload/interval/proxy`。按来源显示字段，`mrs` 只允许 `domain` 和 `ipcidr`。本地文件型规则集需在客户端随配置准备相应文件。 [规则 Provider][mh-rp] |
| 路由规则 | 支持样例所有规则类型，以及 `DOMAIN-KEYWORD/IP-CIDR/IP-CIDR6`，其余类型可通过原始规则录入。支持调序、批量粘贴和查找；`MATCH` 提供移至末尾操作。 [规则语法][mh-rules] |
| 预览、导出和发布 | 结构与引用校验、错误定位、差异预览；一键下载完整 YAML；主动发布版本；按配置创建多个只读订阅链接、复制与吊销。URL 返回 YAML 正文。 |

首期支持额外字段的安全导入、保留与高级编辑。客户端覆写文件生成、执行扩展脚本、任意格式在线转换、代管规则资源和管理员后台作为后续功能候选。

## 5. 验收场景

| 场景 | 验收结果 |
| --- | --- |
| 导入含真实结构的脱敏样例，修改一个 DNS 值后导出 | 各段计数、引用、列表顺序、`no-resolve` 和原有字段保留；只有预期的 DNS 语义变化 |
| 插入规则并调序，或删除被引用的规则集 | 调序结果反映在 YAML 中。失效引用定位到具体规则，修复后可下载及发布 |
| 批量浏览配置 | 73 个组、59 个规则集、74 条规则、95 条 Fake-IP 过滤可搜索和编辑，手机宽度下页面可操作 |
| 分别建立路由器和桌面配置 | 复制后互不影响，退出并重新登录后仍可访问 |
| 验证隔离与发布 | 另一个账户不能读取同一配置；草稿修改不改变订阅内容；发布后同一链接返回新版本；吊销后链接失效 |
| 在目标版本验证两端加载 | 分别用本地文件和 HTTPS 订阅 URL 导入，核对配置解析、策略组、规则、Provider 更新，并记录 OpenClash 覆写与 Clash Verge Rev 扩展前后的生效差异 |

## 6. 原型评审

原型以示例配置演示左侧模块导航、中间编辑和右侧 YAML 预览。手机页面可切到 YAML 视图。建议确认三项交互决策：

1. 新建配置是否继续保留「空白创建」和「导入 YAML」两个入口。
2. 每次修改先保存草稿，再手动发布到现有链接，是否符合更新习惯。
3. DNS、代理组和规则集哪些字段需要直接呈现在常用表单，哪些放在高级编辑中。

[oc-sub]: https://github.com/vernesong/OpenClash/blob/dev/.github/skills/openclash-user-guide/12-subscribe-config.md
[oc-overwrite]: https://github.com/vernesong/OpenClash/blob/dev/.github/skills/openclash-user-guide/11-overwrite-settings.md
[oc-arch]: https://github.com/vernesong/OpenClash/blob/dev/.github/skills/openclash-user-guide/01-architecture.md
[oc-module]: https://github.com/vernesong/OpenClash/blob/dev/.github/skills/openclash-user-guide/16-overwrite-module-format.md
[cv-profile]: https://www.clashverge.dev/guide/profile.html
[cv-extend]: https://www.clashverge.dev/guide/extend.html
[cv-repo]: https://github.com/clash-verge-rev/clash-verge-rev
[mh-config]: https://wiki.metacubex.one/config/
[mh-general]: https://wiki.metacubex.one/config/general/
[mh-dns]: https://wiki.metacubex.one/config/dns/
[mh-proxy]: https://wiki.metacubex.one/config/proxies/
[mh-socks]: https://wiki.metacubex.one/config/proxies/socks/
[mh-dialer]: https://wiki.metacubex.one/config/proxies/dialer-proxy/
[mh-pp]: https://wiki.metacubex.one/config/proxy-providers/
[mh-group]: https://wiki.metacubex.one/config/proxy-groups/
[mh-rp]: https://wiki.metacubex.one/config/rule-providers/
[mh-rules]: https://wiki.metacubex.one/config/rules/
[mh-tun]: https://wiki.metacubex.one/config/inbound/tun/
[mh-sniff]: https://wiki.metacubex.one/config/sniff/
