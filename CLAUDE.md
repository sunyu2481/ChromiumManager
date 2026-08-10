# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概览

Chromium 指纹浏览器环境管理系统。Go 后端（单二进制，内嵌前端）通过命令行开关启动多个隔离的 ungoogled-chromium 实例，每个实例有独立的 user-data-dir、指纹参数、代理和 Cookie。整体打包为一个基于 linuxserver selkies 的 Docker 镜像，用户通过 Web 远程桌面操作。

## 构建与开发

```bash
# 前端（产物落在 src/web/dist，Go 侧靠 //go:embed 引用）
cd src/web && pnpm install && pnpm build

# 后端（必须先有 src/web/dist，否则 go:embed 编译失败）
cd src && CGO_ENABLED=0 go build -ldflags="-s -w" -o manager .

# 前端 lint
cd src/web && pnpm lint:all      # prettier --write + eslint --fix
cd src/web && pnpm dev           # vite dev server（注意下方 CORS 陷阱）

# 完整镜像（需仓库根目录先放好 ungoogled-chromium-*-{amd64,arm64}_linux.tar.gz）
docker build -t chromium-manager .
```

- **无任何测试**：没有 `*_test.go`，也没有前端测试框架。改动靠手动验证。
- `vite-plugin-eslint2` 以 `fix: false` 挂在构建管线里（`src/web/vite.config.js:13`），**lint 报错会直接中断 `pnpm dev` 和 `pnpm build`**。
- CI（`.github/workflows/docker-image.yml`）仅 `workflow_dispatch` 触发，会从同名 tag 的 release 下载 chromium 二进制，再多架构构建推 DockerHub。
- `go.mod` 声明 `go 1.25.7`，Dockerfile 用 `golang:1.26-alpine`。SQLite 驱动是纯 Go 的 `modernc.org/sqlite`，因此可以 `CGO_ENABLED=0`，不要换成 `mattn/go-sqlite3`。

## 运行时拓扑（读单个文件看不出来的部分）

容器暴露的 **3001 是 selkies 远程桌面**，不是本应用的 HTTP 端口。启动链路：

1. `root/defaults/autostart` → `root/scripts/start.sh`
2. `start.sh` 起三个后台进程：tint2 面板、`manager`（Go 服务）、以及一个 `chrome --app=http://127.0.0.1:10101` 的窗口作为管理 UI
3. Go 服务只监听 `127.0.0.1:10101`（`src/main.go:24`），前端 baseURL 硬编码同一地址（`src/web/src/utils/request.js:4`、`src/web/src/utils/constants.js:156`）

所以 **管理界面只能从容器内部访问**，用户看到的是"桌面里的一个 Chromium app 窗口"。被管理的浏览器实例是 `manager` 的子进程，跑在同一个 X display 上——这也是"激活/关闭"能通过 X11 EWMH 实现的前提。

`root/usr/bin/chrome` 是 wrapper：检测 seccomp 后决定是否追加 `--no-sandbox`；`findBrowserPath()`（`src/browser.go:39`）先在 PATH 找 `chrome`，因此拿到的是这个 wrapper 而非 `/opt/chromium/chrome`。

## 后端结构

`src/` 扁平单包（package main）：

| 文件 | 职责 |
|------|------|
| `main.go` | 全部数据结构（Group/Proxy/Profile/FingerprintConfig）、sqids 编解码、路由表、中间件、优雅关闭 |
| `db.go` | SQLite 建表与 PRAGMA |
| `handler_group.go` / `handler_profile.go` / `handler_proxy.go` / `handler_cookie.go` | 按资源分文件的 HTTP handler |
| `auth.go` | 管理面登录页、会话 Cookie、认证中间件与登录限速 |
| `browser.go` | 运行中实例注册表 `runningProfiles` + `runningMu`、浏览器路径查找、参数切分 |
| `sse.go` | 向前端广播运行中 profile ID 列表 |
| `wm_linux.go` / `wm_windows.go` | 构建标签隔离的窗口管理（激活/关闭），Linux 走 xgb + EWMH |

新增接口时：在 `main.go` 的路由块（`:181-206`）注册，handler 放进对应的 `handler_*.go`，前端在 `src/web/src/api/index.js` 加一个导出函数。

## 关键设计约定

**ID 混淆.** 数据库用自增 rowid，对外一律是 sqids 字符串（JSON 字段名是 `_id`）。每个 handler 入口 `decodeID()`、出口 `encodeID()`。`decodeID("")` 和 `decodeID("all")` 都返回 0，而 0 在业务上表示"未分组/无代理"——新增查询过滤时要判 `> 0`，别把 0 当有效外键。

**管理面认证.** `AUTH_PASSWORD` 必须显式配置，`AUTH_USERNAME` 默认为 `admin`；未配置密码时 manager 拒绝启动。未登录请求不能读取管理静态资源、CRUD 接口或 SSE，登录后使用内存会话（12 小时绝对有效期）和 `HttpOnly`、`SameSite=Strict` Cookie。agent/CDP 面仍独立使用 `AGENT_TOKEN` Bearer 认证，不继承管理面会话。

**指纹配置整体存一列 JSON.** `FingerprintConfig` 实现了 `sql.Scanner` / `driver.Valuer`（`src/main.go:100-125`），序列化后进 `profiles.fingerprint` 这一个 TEXT 列。**新增指纹字段只需改结构体 + 前端表单，不需要动 schema 或写迁移。**

**user-data-dir 由指纹 seed 派生，不是 profile ID.** 路径为 `${DATA_DIR}/profiles/encodeID(seed)`（`src/handler_profile.go:247`），删除 profile 时按同一规则清理目录（`:191`）。seed 只在创建时随机生成一次（`enrichFingerprint`，`src/browser.go:76`），更新走的是前端原样回传——**改动 profile 更新链路时务必保证 `fingerprint.seed` 被完整透传，否则会丢失整个浏览器数据目录**。

**指纹开关是 fork 专有的.** `--fingerprint`、`--fingerprint-platform`、`--fingerprint-brand`、`--fingerprint-timezone`、`--disable-fingerprint` 等只有 fingerprint-chromium 分支认识，标准 Chromium 会忽略或报错。拼装逻辑集中在 `launchProfile`（`src/handler_profile.go:268-337`）。

**代理继承.** `proxyLang` / `proxyTimezone` / `proxyLocation` 为 true 时，用关联代理记录上的值覆盖 profile 自身的语言/时区/位置（`src/handler_profile.go:309-320`）。

**Cookie 双向同步.** 启动时若 `Default/Cookies` 不存在，则把 DB 里的 JSON 写成 Chromium 的 SQLite Cookie 库（`:261`）；进程退出后读回并存进 `profiles.cookie`（`:358`）。时间戳需在 Unix 秒与 Chromium 的 1601 纪元微秒之间换算（`src/handler_cookie.go:29-41`）。只处理明文 `value`、不碰 `encrypted_value`——这依赖启动参数里的 `--password-store=basic`，去掉它会导致 Cookie 同步静默失效。JSON 格式对齐 Cookie Editor / EditThisCookie。

**其他行为约定.**
- 删除分组不删配置：事务里把 `profiles.group_id` 置 0（`src/handler_group.go:98`）。
- 启动前清理 `Singleton*` 锁文件（`src/handler_profile.go:254`），避免 Chromium 复用已死实例。
- 自定义启动参数按 `--` 切分（`splitArgs`，`src/browser.go:82`），所以每个参数必须以 `--` 开头，不支持带空格的引号值。
- DB 连接池被限制为单连接（`src/db.go:21`）+ WAL；profile 名在分组内唯一（`src/db.go:71`），违反唯一约束时后端会把错误替换成中文提示。
- 分页 `pageSize` 上限 200，越界回退 10（`src/handler_profile.go:29`）。
- `DATA_DIR` 环境变量决定数据根目录，默认 `data`，容器内为 `/config/data`。

## 前端结构

Vue 3 `<script setup>` + Element Plus，**无 router、无 pinia**。三栏静态布局在 `App.vue` 里直接组合：`LeftIndex`（分组）、`RightIndex`（配置列表 / 代理管理切换）、`MapPicker`（经纬度选点弹窗）。

跨组件状态全靠 `App.vue` 的 `provide/inject`：`activeGroupId`、`device.group`、`getGroupName`、`updateDeviceGroup`。新增共享状态请沿用这个模式，别引入状态库。

`RightContent.vue`（847 行）是核心：配置表格、增删改弹窗、指纹表单、Cookie 导入导出、以及实例运行状态的 SSE 订阅（`:454-469`，连接 `${BASE_URL}/events`，收到的是运行中 ID 数组，转成 `runningSet` 驱动按钮态）。

**axios 和 leaflet 不是 npm 依赖**，而是通过 `index.html` 从 `public/static/js/` 以全局 `<script>` 引入——这也是 eslint 关掉 `no-undef` 的原因（`src/web/eslint.config.js`）。别改成 `import axios from 'axios'`。

所有接口恒返回 HTTP 200，业务状态在 body 的 `code` 字段；axios 响应拦截器只在 `code === 200` 时解包出 `data`，否则 reject（`src/web/src/utils/request.js:12-26`）。

## 陷阱

**`pnpm dev` 直连后端会被 CORS 拦截.** `withMiddleware`（`src/main.go:154`）对 OPTIONS 只回 204，**从不设置任何 `Access-Control-Allow-*` 头**，`vite.config.js` 里也没有 `server.proxy`。因此从 vite dev server（5173）请求 10101 会失败。改前端时要么走完整的 `pnpm build` + `go build` + 内嵌路径验证，要么临时在 `vite.config.js` 加 proxy（别把临时 CORS 补丁提交进后端）。

**`vite.config.js` 里 `base: './'` 是必需的**，因为产物由 Go 的 `http.FileServer` 从 embed FS 提供，绝对路径会 404。
