# Chromium Manager

### Chromium 浏览器环境管理系统

轻松管理多个独立浏览器环境，支持指纹伪装、代理配置、Cookie导入导出等。

[核心功能](#-核心功能) • [界面导览](#-界面导览) • [技术架构](#-技术架构) • [安装指南](#-安装指南) • [使用说明](#-使用说明)

---

## ✨ 核心功能

### 🖥️ 多配置管理
- **独立环境** — 每个配置完全隔离
- **分组管理** — 按业务场景自由分组
- **实例管理** — 启动/激活/关闭 Chromium 实例

### 🎭 指纹伪装
- **随机指纹种子** — 每个配置生成随机指纹种子，确保环境独立
- **平台伪装** — 自定义操作系统、浏览器品牌等平台信息
- **硬件参数** — 自定义 CPU 核心数、设备内存、屏幕分辨率
- **多维指纹** — Canvas、WebGL、Audio、Font、ClientRects、GPU

### 🌐 代理管理
- **代理配置** — 集中管理代理服务器，HTTP、HTTPS、SOCKS4、SOCKS5代理协议（支持用户名、密码认证）
- **IP/语言/时区关联** — 代理可绑定 IP 地址、语言、时区和位置
- **自动继承** — 配置可自动继承关联代理的语言、时区和位置

### 🍪 Cookie 管理
- **扩展兼容** — 兼容 Cookie Editor、EditThisCookie 导入导出格式
- **自动配置** — 浏览器自动导入、导出 Cookie
- **导入导出** — 支持手动导入、导出 Cookie

---

## 📸 界面导览

![管理界面](docs/images/image.png)

![浏览器实例](docs/images/image2.png)

---

## 🏗️ 技术架构

| 组件 | 技术 |
|------|------|
| 前端 | Vite + Vue3 + Element Plus |
| 后端 | Go |
| 数据库 | SQLite |
| 浏览器 | Ungoogled Chromium |

---

## 📦 安装指南

### Docker Compose

启动前请通过宿主机环境变量或 Compose 的 `.env` 文件设置高强度 `AUTH_PASSWORD`。
镜像会自动保护公网 Selkies 入口，外部浏览器登录一次即可进入远程桌面。入口会话 Cookie 保存在外部浏览器中，因此新浏览器或无痕窗口必须单独登录；容器内管理 Chromium 通过 loopback 访问，不会再次弹出登录页。

```yaml
services:
  chromium-manager:
    image: ghcr.io/sunyu2481/chromium-manager:latest
    container_name: chromium-manager
    shm_size: 1gb
    environment:
      - PUID=1000
      - PGID=1000
      - TZ=Asia/Shanghai
      - LC_ALL=zh_CN.UTF-8
      - AUTH_USERNAME=admin
      - AUTH_PASSWORD=${AUTH_PASSWORD:?请先设置 AUTH_PASSWORD}
    ports:
      - 3001:3001
    volumes:
      - ./config:/config
    restart: unless-stopped
```

```bash
docker compose up -d
```

### Docker CLI

以下命令会从宿主机环境读取 `AUTH_PASSWORD`，请先设置该变量。

```bash
docker run -d \
  --name chromium-manager \
  --shm-size=1gb \
  -e PUID=1000 \
  -e PGID=1000 \
  -e TZ=Asia/Shanghai \
  -e LC_ALL=zh_CN.UTF-8 \
  -e AUTH_USERNAME=admin \
  -e AUTH_PASSWORD \
  -p 3001:3001 \
  -v ./config:/config \
  --restart unless-stopped \
  ghcr.io/sunyu2481/chromium-manager:latest
```

### 通过以下网址访问应用

* https://yourhost:3001/

---

## 📖 使用说明

### 创建浏览器配置

1. 在左侧面板创建**分组**
2. 在右侧面板添加新**配置**
3. 配置指纹参数
4. 关联代理（可选）
5. **启动**打开浏览器，**激活**切换到前台，**关闭**关闭浏览器

### 代理配置

1. 进入代理管理页面
2. 添加代理服务器
3. 在配置中选择代理

### Cookie 管理

- **手动导入**: 导入 JSON 格式的 Cookie
- **手动导出**: 导出当前配置的 Cookie

---

## 🤖 Agent 接入（CDP 网关）

容器镜像默认在 `10102` 端口开启 CDP 网关，同一 Docker 网络中的 AI agent（Claude Code、Codex 等）可直接连接已配置好指纹伪装和代理的浏览器实例。

### 与 Agent 容器协同部署

```yaml
services:
  chromium-manager:
    image: ghcr.io/sunyu2481/chromium-manager:latest
    shm_size: 1gb
    environment:
      - PUID=1000
      - PGID=1000
      - TZ=Asia/Shanghai
      - LC_ALL=zh_CN.UTF-8
      - AUTH_USERNAME=admin
      - AUTH_PASSWORD=${AUTH_PASSWORD:?请先设置 AUTH_PASSWORD}
      - AGENT_TOKEN=your-secret-token   # 可选；不设则同网络内免鉴权
    ports:
      - 3001:3001    # selkies 远程桌面
      # 10102 不 publish 到宿主机，仅 docker 网络内可达
    volumes:
      - ./config:/config
    networks: [agentnet]
    restart: unless-stopped

  claude-code:
    image: your/claude-code-image
    networks: [agentnet]

networks:
  agentnet:
```

### 以配置名称定位浏览器

Agent 接口（`/agent/*`、`/cdp/*`）一律使用**配置名称**定位浏览器，不使用内部 ID。配置名称全局唯一，不同分组之间也不能重名。如果从旧版本升级时库里已有跨分组的同名配置，agent 访问这些名称会返回 409，在管理界面改名后重启容器即可。

名称含中文、空格等字符时，放进地址前需要做 URL 编码（例如 `香港 01` 对应 `/cdp/%E9%A6%99%E6%B8%AF%2001`）。`acquire` 返回的地址已经编码好，可以直接使用。

### 获取可用浏览器列表

```bash
curl -H "Authorization: Bearer your-secret-token" \
     http://chromium-manager:10102/agent/browsers

# 响应示例（group 为分组名称，未分组为空；cdpUrl/wsUrl 仅在 CDP 就绪后出现）
# {"code":200,"data":[{"name":"HK-01","group":"电商","running":true,"cdpReady":true,
#   "cdpUrl":"http://chromium-manager:10102/cdp/HK-01",
#   "wsUrl":"ws://chromium-manager:10102/cdp/HK-01/devtools/browser","clients":1}]}
```

### 按需获取浏览器（自动启动 + 等待 CDP 就绪）

```bash
# 按名称获取（若未运行则自动启动）
curl -H "Authorization: Bearer your-secret-token" \
     -X POST http://chromium-manager:10102/agent/acquire \
     -H "Content-Type: application/json" \
     -d '{"name": "HK-01"}'

# 响应示例
# {"code":200,"data":{"name":"HK-01","cdpUrl":"http://chromium-manager:10102/cdp/HK-01",
#   "wsUrl":"ws://chromium-manager:10102/cdp/HK-01/devtools/browser","started":true}}
```

`cdpUrl` 和 `wsUrl` 都只由名称决定。`wsUrl` 是 browser 级的固定 WebSocket 地址，代理会自动转发到当前实例，浏览器重启后依然有效。

### 接入 Claude Code（通过 chrome-devtools-mcp）

chrome-devtools-mcp 的 `--browser-url` 会丢掉地址里的 `/cdp/<名称>` 路径段，所以要改用 `--wsEndpoint` 连固定的 `wsUrl`。这样只需注册一次：

```bash
claude mcp add chrome -- npx chrome-devtools-mcp@latest \
  --wsEndpoint ws://chromium-manager:10102/cdp/HK-01/devtools/browser
# 设置了 AGENT_TOKEN 时追加：--wsHeaders '{"Authorization":"Bearer your-secret-token"}'
```

### 接入 Playwright 脚本

```js
const { chromium } = require('playwright')
const browser = await chromium.connectOverCDP(
  'http://chromium-manager:10102/cdp/HK-01'
)
// browser.contexts()[0] 即带完整 Cookie 与指纹的持久化上下文
const page = await browser.contexts()[0].newPage()
await page.goto('https://example.com')
```

### 使用完毕后释放

```bash
# 仅解除占用，保持浏览器运行
curl -X POST http://chromium-manager:10102/agent/release \
     -d '{"name":"HK-01"}'

# 同时关闭浏览器
curl -X POST http://chromium-manager:10102/agent/release \
     -d '{"name":"HK-01","stop":true}'
```

### 管理界面操作

浏览器运行时，操作列的更多菜单（⋯）中有 **复制 CDP 地址**，复制的是按名称寻址的接入地址。

### 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `AUTH_USERNAME` | `admin` | 公网入口登录用户名 |
| `AUTH_PASSWORD` | 无（必填） | 公网入口登录密码；未设置时容器初始化会拒绝继续 |
| `AGENT_ADDR` | `0.0.0.0:10102` | Agent 面监听地址；设为空值可完全禁用 |
| `AGENT_TOKEN` | 空（不鉴权） | 非空时 agent 接口要求 `Authorization: Bearer <token>` |
| `AGENT_OPERATION_LIMIT` | `32` | Agent 同时处理的 `browsers/acquire/release` 请求数 |
| `CDP_MAX_CLIENTS_PER_PROFILE` | `32` | 单个 profile 同时允许的 CDP 连接数 |
| `LISTEN_ADDR` | `127.0.0.1:10101` | 管理面监听地址（管理 UI 与 CRUD）；服务只接受 loopback 请求 |

Selkies 页面、WebSocket 和文件入口均通过 nginx `auth_request` 校验 Go 会话，不使用基础镜像的 Basic Auth。公网部署必须使用 HTTPS，建议再使用支持 MFA 的反向代理，且不要将 `10102` 发布到公网。

---

## 📄 许可证

MIT License
