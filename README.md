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

```bash
docker run -d \
  --name chromium-manager \
  --shm-size=1gb \
  -e PUID=1000 \
  -e PGID=1000 \
  -e TZ=Asia/Shanghai \
  -e LC_ALL=zh_CN.UTF-8 \
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

### 获取可用浏览器列表

```bash
curl -H "Authorization: Bearer your-secret-token" \
     http://chromium-manager:10102/agent/browsers
```

### 按需获取浏览器（自动启动 + 等待 CDP 就绪）

```bash
# 按名称获取（若未运行则自动启动）
curl -H "Authorization: Bearer your-secret-token" \
     -X POST http://chromium-manager:10102/agent/acquire \
     -H "Content-Type: application/json" \
     -d '{"name": "HK-01"}'

# 响应示例
# {"code":200,"data":{"id":"Xk3mP9qR","cdpUrl":"http://chromium-manager:10102/cdp/Xk3mP9qR","started":true}}
```

### 接入 Claude Code（通过 chrome-devtools-mcp）

```bash
claude mcp add chrome -- npx chrome-devtools-mcp@latest \
  --browser-url http://chromium-manager:10102/cdp/Xk3mP9qR
```

### 接入 Playwright 脚本

```js
const { chromium } = require('playwright')
const browser = await chromium.connectOverCDP(
  'http://chromium-manager:10102/cdp/Xk3mP9qR'
)
// browser.contexts()[0] 即带完整 Cookie 与指纹的持久化上下文
const page = await browser.contexts()[0].newPage()
await page.goto('https://example.com')
```

### 使用完毕后释放

```bash
# 仅解除占用，保持浏览器运行
curl -X POST http://chromium-manager:10102/agent/release \
     -d '{"id":"Xk3mP9qR"}'

# 同时关闭浏览器
curl -X POST http://chromium-manager:10102/agent/release \
     -d '{"id":"Xk3mP9qR","stop":true}'
```

### 管理界面操作

浏览器运行时，操作列会出现 **CDP** 按钮，点击可直接复制当前实例的 CDP 接入地址。

### 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `AGENT_ADDR` | `0.0.0.0:10102` | Agent 面监听地址；设为空值可完全禁用 |
| `AGENT_TOKEN` | 空（不鉴权） | 非空时 agent 接口要求 `Authorization: Bearer <token>` |
| `LISTEN_ADDR` | `127.0.0.1:10101` | 管理面监听地址（管理 UI 与 CRUD，默认不出容器） |

---

## 📄 许可证

MIT License
