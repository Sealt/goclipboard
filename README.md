<div align="center">

<img src="docs/logo.svg" width="120" alt="GoClipboard" />

# GoClipboard

**一个轻量、实时协作的临时云剪贴板 —— 单个二进制即可自托管。**

分享文本和文件，只需要一个 URL。内容实时同步，到期自动销毁。

[English](README.en.md) | 简体中文

![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)
![Docker](https://img.shields.io/badge/Docker-scratch%20镜像-2496ed?logo=docker&logoColor=white)
![CRDT](https://img.shields.io/badge/同步-RGA%20CRDT-7c3aed)
![PRs](https://img.shields.io/badge/PRs-welcome-brightgreen)
[![LinuxDo](https://img.shields.io/badge/LinuxDo-%E5%8F%8B%E6%83%85%E9%93%BE%E6%8E%A5-fd4d2b)](https://linux.do)

</div>

---

## 为什么需要它？

密码、SSH key、一行命令、配置片段、随手要发的截图——把内容从一台机器弄到另一台机器，不应该需要注册账号、登录流程，或者再装一个记笔记的 App。

GoClipboard 给你一个 **URL**：把内容贴进房间，分享链接，所有打开的人实时看到同步结果。它到期自动清理、没有数据库、整个服务就是一个静态二进制，跑在任何机器上。

## ✨ 特性

- ⚡ **单二进制、零运行时依赖** —— Go 静态编译约 10 MB，Docker 镜像直接基于 `scratch`
- 🔗 **URL 即分享** —— 每个剪贴板随机生成 key，TTL 按分钟/小时/天设定，到期自动清理
- 👥 **实时协作编辑** —— 基于字符级 **RGA CRDT**，通过 WebSocket 同步：多人并发编辑自动合并，互不覆盖，弱网也不怕
- 🖱️ **远程光标与选区** —— 实时看到协作者的输入位置，每人一个专属颜色
- 🔒 **只读模式** —— 链接手动加 `?view=true` 即为只读（服务端强制拒绝一切写入 op），适合需要"只读分享"的场景；分享面板默认只给房间链接，访问控制交给房间密码
- 🔑 **房间密码** —— 手动输入或自动生成；选择验证范围：**仅编辑需密码**（查看公开），或**查看和编辑都需密码**（未解锁看不到任何内容）
- ↩️ **撤销 / 重做** —— 基于 CRDT 逆操作的协作式撤销（Ctrl+Z / Ctrl+Shift+Z），不受他人并发编辑干扰
- 🕘 **版本历史** —— 服务端共享存档（编辑自动记录，最多 20 条），可预览/恢复/清空；有房间密码时查看历史也需密码；历史会保留已删除内容，分享前请清空或上锁
- 📝 **Markdown 预览** —— 所见即所得渲染 + 代码高亮（纯前端、输出经过消毒，不怕恶意粘贴）
- 🔗 **二维码分享** —— 弹窗内一键生成房间二维码（默认编码房间链接，可加 `mode` 参数），手机扫码即开
- 📁 **房间文件分享** —— 拖入文件即得链接；落盘存储，每个文件独立下载密码，上传/删除需管理员密码
- 🔐 **密码只存哈希** —— 房间密码与文件密码均仅保存 salt + SHA-256（永不落盘明文）
- 💾 **内容持久化** —— 房间以 CRDT 快照落盘（默认 `data/rooms`），重启自动恢复；`PERSIST_DIR=off` 可关回纯内存
- ⌨️ **CLI 客户端** —— 同一个二进制即可 `push` / `pull`：`echo hi | goclipboard push` 直接拿链接
- 🌙 **暗色主题 + 中英文界面** —— 跟随系统或手动切换，一键换语言
- 🚦 **防滥用** —— 按 IP 限流 + 自适应黑名单，扫描者自动封禁 30 分钟
- 🧹 **内存自限** —— 内存预算封顶；写满时返回 `HTTP 507` + `Retry-After`
- 📊 **可运维** —— 结构化 JSON 日志、`/healthz` 健康检查、优雅停机
- 🐳 **部署简单** —— 多架构 Docker 构建，`docker compose up` 一条命令
- 🇨🇳 **中文界面** —— 简洁的单页前端，无框架、无构建步骤

## 🖼️ 界面截图

<img src="docs/screenshot.png" width="720" alt="GoClipboard 编辑器：实时 CRDT 内容、TTL 选择、在线成员与同步状态" />

## 🚀 快速开始

```sh
go run .
# 或
make run
```

打开 **http://localhost:8080** —— 自动跳转到一个全新的剪贴板，完事。

```sh
docker compose up -d          # 用 Docker 自托管
```

### 命令行客户端

同一个二进制既是服务端也是客户端（`-url` 或环境变量 `GOCLIPBOARD_URL` 指定服务器，默认 `http://localhost:8080`）：

```sh
echo "hello" | goclipboard push            # → 打印房间链接
goclipboard push -ttl 2h notes.txt         # 读文件、2 小时后过期
goclipboard push -v notes.txt              # 顺带打印只读链接
goclipboard push -password 'pw' -key AbC123 notes.txt  # 写入已锁定房间
goclipboard pull https://host/AbC123       # 拉取内容到 stdout
goclipboard pull -o out.txt AbC123         # 写入文件
goclipboard pull -password 'pw' AbC123     # 查看范围密码保护的房间
```

无 `-key` 时服务器自动生成房间 key（`POST /api/clipboard`）。

## 🧠 工作原理：CRDT，而不是后写覆盖

文本编辑通常是场"打架"：两个人同时输入，后写的人覆盖先写的人。GoClipboard 用**字符级 CRDT**（insert-after RGA）绕开了这个问题：

- 每个字符是一个原子：`id = clientId:clock`（Lamport 时间戳），带 `parent` 指针。
- 并发插入在 **Unicode 码点**粒度合并 —— 不存在整串后写覆盖。
- 兄弟节点顺序确定：clock 大者在前，再按站点 id 排序，所以中段插入总是待在光标旁边。
- 删除是墓碑（tombstone），房间 TTL 到期时统一回收。
- 实时输入以 WebSocket **ops** 增量同步；迟到者收到完整 **state** 快照；断线重连自动合并。
- REST `PUT` 仍可整篇替换（支持 `baseVersion` 乐观并发，冲突返回 `409`），适合简单客户端和离线兜底。

这也是弱网下依然稳定的原因：op 幂等、与顺序无关、重发成本极低。

## 🔌 API

### 剪贴板（REST）

```http
POST   /api/clipboard                  # 创建房间（key 由服务端生成）→ 返回含 key/viewKey
GET    /api/clipboard/{key}            # 读取内容 + 版本 + viewKey
PUT    /api/clipboard/{key}            # 整篇替换
DELETE /api/clipboard/{key}            # 删除
GET    /api/clipboard/{key}/history    # 版本历史（有房间密码时需密码）
POST   /api/clipboard/{key}/history    # 手动存档当前内容
DELETE /api/clipboard/{key}/history    # 清空历史
```

```sh
curl -X PUT localhost:8080/api/clipboard/AbC123 \
  -H 'Content-Type: application/json' \
  -d '{"content":"hello 世界","ttlSeconds":3600,"clientId":"my-site"}'
```

响应中的 `viewKey` 用于构造只读链接 `/{key}?view={viewKey}`：带该参数的页面处于只读模式，WebSocket 会话也只读（服务端拒绝一切写入 op）。

`PUT` 支持可选的 `baseVersion` 乐观并发：过期覆盖会被拒绝并返回 `409`（附当前状态），离线/REST 客户端可以据此合并而不是盲目覆盖。

### 房间密码

分享弹窗里可设置房间密码（**手动输入或一键重新生成**），并选择验证范围：

- **编辑**（默认）—— 内容查看公开；编辑（内容写入、删除、改密码、**查看/清空历史**）需要密码
- **查看** —— 查看和编辑都需要密码：未解锁的会话收不到任何内容，文件列表与下载同样受保护

密码由设置者保管，服务端绝不回传（响应里只有 `passwordSet` / `passwordScope`），持有链接的人也无法通过删掉参数拿到它。未锁定房间的任意链接持有者都可以设置密码并锁定（claim）；分享前建议先设好密码。

```http
GET /api/clipboard/{key}/password          # → {"passwordSet":true,"scope":"view"}
PUT /api/clipboard/{key}/password          # 设置/修改/解除
# 修改或解除时 currentPassword 必填（与旧密码一致）；scope: "edit" | "view"
```

查看「查看范围」保护的房间需携带密码（优先 `X-Goclip-Password` 请求头；也接受 `?password=` 但可能进访问日志），否则 `GET` 返回 `401`；WebSocket 会话先收到不含正文的锁定帧（`passwordRequired: true`），再发送 `{"type":"auth","password":"..."}` 解锁，密码错误会收到 `invalid view password` 错误帧。

### 版本历史

服务端按房间保存最多 20 条内容快照（编辑自动记录，5s 节流；可手动存档）。**快照会保留后来被删除/覆盖的正文**，因此：

- 有房间密码时，`GET/POST/DELETE …/history` 都需要密码（`X-Goclip-Password`）
- 分享敏感内容前请「清空历史」，或使用查看范围密码
- 历史占用内存预算（计入 `MAX_MEMORY_MB`），近满时可能因历史增长返回 `507`

### 实时同步（WebSocket）

```http
GET /api/clipboard/{key}/ws?clientId={id}
# 只读会话：
GET /api/clipboard/{key}/ws?clientId={id}&view={viewKey}
```

**服务端 → 客户端**

| type     | 含义                                              |
|----------|---------------------------------------------------|
| `state`  | 完整 CRDT 快照：`items`、线性化 `content`、版本号、`viewKey` |
| `ops`    | 已应用的 op 批次 + `version` / `content`          |
| `cursor` | 远程光标 / 选区（码点偏移）                       |
| `files`  | 文件列表元数据（上传/删除/过期后推送）            |

**客户端 → 服务端**

```json
{"type":"ops","ops":[{"op":"ins","id":"a:1","after":"","ch":"x"}],"ttlSeconds":3600,"password":"房间密码(已锁定房间)"}
{"type":"cursor","cursorPos":0,"selectionEnd":0,"color":"#61afef"}
{"type":"auth","password":"房间密码"}   // 查看范围密码：解锁内容推送
```

op 类型：`ins`（`id`、`after`、`ch` —— 恰好一个码点）与 `del`（`id`）。

**限制：** 内容 ≤ 1 MiB · 每批 ≤ 4096 ops · 每条 WS 消息 ≤ 256 KiB。

### 文件分享

| 操作                                  | 鉴权                                  |
|---------------------------------------|---------------------------------------|
| `GET  /files`                         | 公开（有房间 URL 即可）               |
| `POST /files`（multipart）            | 房间开放 → 文件密码；未开放 → 管理员密码 + 文件密码 |
| `GET  /files/{id}`                    | 该文件的密码（`X-File-Password` 或 `?filePassword=`；`?password=` 仅表示房间密码） |
| `DELETE /files/{id}`                  | 管理员密码                            |
| `GET/PUT /settings`（上传开关）       | 管理员密码                            |

房间默认**关闭上传**。管理员**三击房间名**（路径）即可切换开关，设置持久化在磁盘上。文件随房间 TTL 一起过期。管理员密码来自 `UPLOAD_PASSWORD`；留空则整个文件功能停用。

### 健康检查

```http
GET /healthz
```

Docker 部署时容器健康检查直接调用二进制自身（scratch 镜像没有 curl/wget）：

```sh
/goclipboard -healthcheck   # 探测本机 /healthz，成功退出码 0
```

## ⚙️ 配置

全部通过环境变量：

| 变量                    | 默认值    | 说明                                                        |
|-------------------------|-----------|-------------------------------------------------------------|
| `PORT`                  | `8080`    | 监听端口                                                    |
| `MAX_ROOMS`             | `10000`   | 最大存活房间数（内存中）                                    |
| `MAX_MEMORY_MB`         | `256`     | 内容 + CRDT 原子 + 版本历史的内存预算，超出返回 `507`       |
| `UPLOAD_PASSWORD`       | _(空)_    | 文件上传/删除的管理员密码；留空停用文件功能                  |
| `FILE_DIR`              | `data/files` | 上传文件的磁盘根目录（`{FILE_DIR}/{room}/{id}.bin`）     |
| `PERSIST_DIR`           | `data/rooms` | 房间持久化目录；CRDT 快照落盘（每房一文件，~250ms 防抖），重启自动恢复。设为 `off` / `none` / `-` 则纯内存、到期即焚 |
| `TRUSTED_PROXIES`       | _(空)_    | 可信反向代理 CIDR 列表（逗号分隔，如 `127.0.0.1/32,10.0.0.0/8`）。为空时**不信任任何转发头**（`CF-Connecting-IP`/`X-Forwarded-For`/`X-Real-IP` 一律忽略，限流/黑名单按直连 IP 计算），防止伪造头绕过限流或误封他人。Cloudflare 部署请填入其官方 IP 段（https://www.cloudflare.com/ips-v4、ips-v6），并优先使用 `CF-Connecting-IP`（Cloudflare 对 `X-Forwarded-For` 是追加而非覆写，链首可能被客户端伪造） |
| `MAX_WS_CONNS`          | `512`     | WebSocket 全局并发连接上限，超出返回 `503`                  |
| `MAX_WS_CONNS_PER_IP`   | `32`      | 单 IP 的 WebSocket 并发连接上限                             |
| `WS_MSG_RATE`           | `50`      | 单连接入站消息令牌速率（条/秒）；超限断开连接（客户端会自动重连并同步） |
| `WS_MSG_BURST`          | `100`     | 单连接入站消息突发预算                                      |

运维默认值：限流 10 req/s（burst 20）、自适应黑名单（硬阈值 5、扫描阈值 10、软阈值 30、窗口 30 s、硬封禁 30 分钟、软封禁 5 分钟；400/405/413 属软错误 —— 客户端 bug 或密码手滑不会把整个 NAT 封死，414 与扫描探测走硬封禁）、每分钟清理一轮、优雅停机 10 秒。WebSocket 通道有连接数上限与单连接消息限速（默认值对正常多人编辑留有充足余量：客户端打字约 17 msg/s + 光标约 13 msg/s）。

> 部署在反向代理后面时，请把代理地址加入 `TRUSTED_PROXIES`，并让代理**覆写**（而非追加）`X-Forwarded-For`（nginx 示例：`proxy_set_header X-Forwarded-For $remote_addr;`）。Cloudflare 无需额外配置：其 `CF-Connecting-IP` 头部会被优先采用。部署专属值（`UPLOAD_PASSWORD`、`TRUSTED_PROXIES`）建议写在 compose.yaml 同目录的 `.env` 文件中（docker compose 自动读取，也不会进入 git）。

## 🐳 Docker

```sh
docker compose up -d
```

多架构（`linux/amd64`、`linux/arm64` 等）`scratch` 镜像——无 shell、无 libc，只有二进制和 CA 证书。自带健康检查；命名卷持久化上传文件与房间快照（compose.yaml 使用 `PERSIST_DIR=/data/rooms`）。

## 🛠️ 开发

```sh
make run          # 开发服务器 :8080
make test         # go test ./...
make test-js      # 前端 crdt.js 交叉收敛测试（需 node）
make test-cover   # 覆盖率报告 → coverage.html
make build        # 编译单个静态二进制
```

代码量小，结构刻意保持朴素：

```
main.go               入口：配置、CLI 分发（push/pull）、中间件、优雅停机
internal/crdt/        RGA 序列 CRDT（插入 / 删除 / 物化 / 快照）
internal/store/       内存房间存储 + 磁盘文件存储、可选持久化、TTL 清理
internal/handler/     HTTP + WebSocket 路由、文件上传下载、只读会话
internal/middleware/  限流、黑名单、安全头、请求日志
internal/cli/         push/pull 客户端模式（同一二进制）
static/               前端：app.js + crdt.js（原生 JS，无构建步骤；vendor/ 为 MIT 单文件库）
```

## 📜 License

本项目基于 [MIT](LICENSE) 协议开源 —— 详见 [LICENSE](LICENSE) 文件。
