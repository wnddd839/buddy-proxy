<p align="center">
  <img src="docs/logo.svg" width="88" height="88" alt="Buddy Proxy" />
</p>

<h1 align="center">Buddy Proxy</h1>

<p align="center">
  <strong>把你的 CodeBuddy / WorkBuddy 账号，变成任何 OpenAI 客户端都能顺手直连的 <code>/v1</code> 渠道。</strong>
</p>

<p align="center">
  <a href="https://wnddd839.github.io/buddy-proxy/"><img src="https://img.shields.io/badge/Product-Page-1C1C1C?style=flat-square" alt="Product Page" /></a>
  <a href="https://github.com/wnddd839/buddy-proxy/releases/latest"><img src="https://img.shields.io/github/v/release/wnddd839/buddy-proxy?label=Latest%20release&style=flat-square&color=1C1C1C" alt="Latest release" /></a>
  <a href="https://github.com/wnddd839/buddy-proxy/blob/main/LICENSE"><img src="https://img.shields.io/badge/License-BSD--3--Clause-1C1C1C?style=flat-square" alt="License" /></a>
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-%E2%89%A51.26-1C1C1C?style=flat-square&logo=go&logoColor=white" alt="Go" /></a>
  <img src="https://img.shields.io/badge/UI-Atelier%20%2F%20Swiss%20v0.8-1C1C1C?style=flat-square" alt="Atelier / Swiss UI" />
</p>

<p align="center">
  <a href="#v080-全新管理台">v0.8 全新 UI</a> ·
  <a href="#三分钟跑起来">三分钟跑起来</a> ·
  <a href="#接入你的客户端">接入客户端</a> ·
  <a href="#文档与进阶">文档</a> ·
  <a href="#致谢">致谢</a> ·
  <a href="#免责声明与合规">免责声明</a>
</p>

> **v0.8.0 现已发布**：管理台从 4,000 行手写 HTML 彻底进化为 **Vite + React 19 + TypeScript + Tailwind v4** 打造的 **Atelier / Swiss 四主题工作台**，首屏趋势折线图支持「全部 90 天」历史回看——而它依然是一个开箱即用的单文件 Go 二进制，零外链、离线秒开。详见 [更新日记](CHANGELOG.md)。
>
> 同系列姊妹项目 `kiro-proxy` 也已发布，欢迎一起体验。

---

## 它解决什么问题？

你手里有腾讯 CodeBuddy 或 WorkBuddy 账号，也有一桌子只认 OpenAI `/v1` 接口的顺手工具——Codex CLI、Claude Code、NewAPI、ZCode、Sub2API，或者你自己写的脚本和 SDK。

你不想为了调个接口在后台挂着臃肿的 IDE、折腾浏览器插件，也不想每换一个客户端就重写一遍适配。

**Buddy Proxy 就是中间那个轻巧、省心的协议翻译官。**

```text
你的客户端（任意 OpenAI / Responses 工具）
        │
        ▼  标准 /v1 接口
   Buddy Proxy（单文件 Go 服务 · 多账号调度 · 全新可视化管理台）
        │
        ▼  OAuth 协议直连（protocol_direct）
CodeBuddy / WorkBuddy 官方上游（国内 & 国际同进程并存）
```

下载一个十几兆的二进制（名仍为 `codebuddy-proxy`），跑在你自己的电脑、NAS 或云服务器上，点两下完成 OAuth 授权，就能立刻接进整个 OpenAI 生态。

---

## v0.8.0 全新管理台

以前的管理台能用，但越加功能越像一张密密麻麻的表单。**在 v0.8.0，我们把整个前端推倒重写了。**

- **四套精调主题，白天深夜都耐看**
  - **温润纸刊 · 亮**（Atelier Light） / **暗墨工坊 · 暗**（Atelier Dark）——带点纸质编辑感，久看不累
  - **精密冷白 · 亮**（Swiss Light） / **深邃黑曜 · 暗**（Swiss Dark）——干脆利落的工程仪表盘风格
  - 自动跟随系统深浅色，也可以在界面里一键切换并记住偏好。
- **打开总览，不再是一堵卡片墙**
  - 首屏用扁平细线指标条 + **全宽平滑曲线面积图**直观展示 Token 消耗与缓存命中率，告别视觉噪音。
  - 支持 **今日 / 近 7 天 / 近 30 天 / 全部（最长 90 天保留）** 自由切换，拉长周期也能一眼看清用量走势。
- **固定侧边栏 + 7 大清晰视图**
  - 宽屏下左侧导航常驻固定、右侧内容独立滚动，顶部 48px 轻量状态条里**国内 / 国际**、**CodeBuddy / WorkBuddy** 随时一键切换、绝不折行。
  - `统计总览` · `请求明细` · `账号池与授权` · `自动签到` · `模型与路由` · `System Prompt` · `客户端接入` 各归其位；教程和次要说明收进侧滑抽屉，版本自更新静静放在侧边栏底部。
- **依然单文件、零外部依赖**
  - 新管理台虽然用上了现代前端工程栈，构建时仍会压成一份自包含 HTML 内嵌进 Go 二进制里——**不请求任何外部 CDN 或 Web Font，内网离线环境照样毫秒级打开。**

---

## 核心能力

### 1. 完整的 OpenAI 协议家族
- **Chat Completions**（`/v1/chat/completions`）：流式 / 非流式全支持，完整透传 Token 用量与缓存命中统计，流式收尾自动补齐 usage chunk。
- **Responses API**（`/v1/responses`）：**Codex CLI 可直接连**，支持 `input` item 数组、工具调用、结构化图片/截图、`reasoning.effort` 与命名 SSE 事件。
- **图片生成与编辑**（`/v1/images/generations` · `/v1/images/edits`）：原生对接上游文生图与图生图模型。
- **官方联网搜索闭环**：显式开启 `web_search_options` 或 `web_search` 时自动调用账号官方搜索能力（不声明不触发），同时暴露 `/v1/search`、`/v1/webfetch`、`/v1/agentic_search`，详见 [搜索接入指南](docs/guides/search.md)。
- **模型目录**（`/v1/models`）：实时合并上游目录，60 秒缓存并支持一键刷新。

### 2. 懂分寸的多账号调度
- **双站双产品，一个进程全管**：国内站 / 国际站账号同时在线；CodeBuddy / WorkBuddy 共用同一套 OAuth token，按需切产品头和模型目录。单请求还能用不同 API Key、`X-Site` 或 `cn:` / `global:` 前缀精准指路。
- **会话亲和 + 智能选号**：同一场对话尽量钉在同一账号上，吃满上游缓存；新会话自动挑剩余 Credits 最充裕的号。支持设置**选号积分下限**，请求后本地实时扣减余额，低于阈值立即切号。
- **精准模型级冷却与备用路由**：某个模型触发限流（6004 / 429）只冷却该模型、不误伤同账号其他模型，按上游中英文重置时间自动回血；支持有方向的模型路由与「源区用尽再跨区」备用链。
- **固定账号 & 固定出口**：可以把某把 API Key 钉死在指定账号上；也能给不同账号绑定各自的 HTTP / HTTPS / SOCKS5 代理出口（代理失效绝不静默回退直连），还支持为国际站 OAuth 单独指定代理出口。

### 3. 省心的日常运维套件
- **一键批量导入**：粘贴客户端凭据或旧账号池 JSON 就能批量入池，自动识别站点、重复账号原地更新。
- **每账号自动签到**：按北京时间单独配置固定时刻、窗口随机、全天分布或整批错开，带失败重试与 48h 审计记录。
- **System Prompt 开发者面板**：提供 `auto` / `keep` / `fold` / `strip` 四档热切换，优雅绕过上游 `11128` 身份句拦截，全程只存哈希指纹、不落盘任何对话明文。
- **为真实长对话兜底**：两档可调流停滞看门狗（默认各 10 分钟，长思考不误杀）、断流防静默截断、可选波动监测（Webhook / SMTP 邮件告警）、以及管理台内带 SHA-256 校验与失败自动回滚的一键自更新。

---

## 三分钟跑起来

### 方式一：下载单文件二进制（最简单）

去 [GitHub Releases](https://github.com/wnddd839/buddy-proxy/releases/latest) 拿对应系统的文件，直接运行就行：

| 平台 | 文件名 |
| :--- | :--- |
| **Windows 64 位** | `codebuddy-proxy-windows-x64.exe` |
| **Linux 64 位** | `codebuddy-proxy-linux-amd64` |
| **Linux ARM64** | `codebuddy-proxy-linux-arm64` |
| **Linux ARMv7 / ARMv6** | `codebuddy-proxy-linux-armv7` / `codebuddy-proxy-linux-armv6` |
| **macOS Apple 芯片** | `codebuddy-proxy-darwin-arm64` |
| **macOS Intel** | `codebuddy-proxy-darwin-amd64` |

```powershell
# Windows
.\codebuddy-proxy-windows-x64.exe
```

```bash
# Linux / macOS
chmod +x ./codebuddy-proxy-linux-amd64
./codebuddy-proxy-linux-amd64
```

首次启动会自动生成一把 API Key 并写进配置里（日志会直接打印出来），不需要手搓任何配置文件。

### 方式二：Docker Compose

仓库已备好非 root 多阶段 `Dockerfile`、`compose.yaml` 与 GHCR 多架构镜像（`amd64` / `arm64` / `armv7`）：

```bash
docker compose up -d
```

详细持久化、环境变量和反代建议见 [Docker 部署指南](docs/guides/docker.md)。

### 方式三：从源码运行

需要 Go ≥ 1.26：

```bash
git clone https://github.com/wnddd839/buddy-proxy.git
cd buddy-proxy
go run ./cmd/codebuddy-proxy
```

### 跑起来之后的 3 步

启动后你会看到三个本地入口：
- **管理台**：`http://127.0.0.1:32126/direct-admin/`
- **OpenAI API**：`http://127.0.0.1:32126/v1`
- **健康探针**：`http://127.0.0.1:32126/health`（存活）· `/readyz`（上游可达）

接着只需三步：
1. 打开管理台，顶部选好「国内 / 国际」和「CodeBuddy / WorkBuddy」，在账号池里点 **开始 OAuth** 完成浏览器授权；
2. 回到页面点 **检查登录**，账号余额与状态立即就位；
3. 去「客户端接入」页复制 **Base URL + API Key**，贴进你常用的客户端就能开聊。

> **关于升级**：二进制部署后续直接在管理台侧边栏点「检查更新 / 更新并重启」即可，走官方 Release + `SHA256SUMS.txt` 校验，只替换可执行文件，绝不动你的 `.env`、账号池和历史记录；Linux systemd 托管也可以配合仓库里的 `scripts/update-linux-systemd.sh` 自动备份和健康回滚。Docker 用户执行 `docker compose pull && docker compose up -d`。

---

## 接入你的客户端

### 通用 OpenAI 客户端

NewAPI、ZCode、Cherry Studio、NextChat 或任意 OpenAI SDK，按下面填就行：

```text
Base URL : http://127.0.0.1:32126/v1
API Key  : 管理台复制的 Key
Model    : auto  或 GET /v1/models 返回的任意模型 id
```

快速测一下连通性：

```bash
curl http://127.0.0.1:32126/v1/chat/completions \
  -H "Authorization: Bearer $CODEBUDDY_PROXY_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"auto","stream":true,"messages":[{"role":"user","content":"你好，报个到"}]}'
```

> 请求时 `auto` 和 `codebuddy/auto` 都认；列表接口为 `GET /v1/models`（暂不提供 `GET /v1/models/{id}` 单查）。

### Codex CLI（Responses API 直连）

在 `~/.codex/config.toml` 里加上这段：

```toml
model_provider = "buddy-proxy"
model = "auto"

[model_providers.buddy-proxy]
name = "Buddy Proxy"
base_url = "http://127.0.0.1:32126/v1"
env_key = "CODEBUDDY_PROXY_API_KEY"
wire_api = "responses"
```

```bash
export CODEBUDDY_PROXY_API_KEY=cbp_xxx
codex "帮我看看这个仓库的结构"
```

### 常用多区 / 多 Key 玩法

大部分配置都能在管理台里直接点完热生效；如果喜欢写 `.env`，这里是最常用的几项：

```env
# 站点与产品（同一套 token，自由正交组合）
CODEBUDDY_SITE=domestic                    # domestic | global
CODEBUDDY_INTERNET_ENVIRONMENT=internal    # internal | public
CODEBUDDY_PRODUCT=codebuddy                # codebuddy | workbuddy

# 多 Key 绑定区域或指定账号（换 Key 即换区，模型列表干净无重复前缀）
# CODEBUDDY_PROXY_API_KEYS=cbp_aaa:global,cbp_bbb:domestic,cbp_fixed:account=<账号id>

# 可选：开启每账号自动签到（具体时间在管理台配置）
# CODEBUDDY_PROXY_CHECKIN_AUTO=true
```

全部环境变量、流超时、请求体上限与出口代理说明，都整理在 [配置参考](docs/guides/configuration.md) 里。

---

## 文档与进阶

| 想看什么 | 直达链接 |
| :--- | :--- |
| 产品介绍页 | [wnddd839.github.io/buddy-proxy](https://wnddd839.github.io/buddy-proxy/) |
| 快速上手 | [`docs/guides/getting-started.md`](docs/guides/getting-started.md) |
| 完整配置参考 | [`docs/guides/configuration.md`](docs/guides/configuration.md) |
| HTTP API 契约 | [`docs/api/http.md`](docs/api/http.md) |
| 官方搜索接入 | [`docs/guides/search.md`](docs/guides/search.md) |
| Docker 部署 | [`docs/guides/docker.md`](docs/guides/docker.md) |
| 波动监测告警 | [`docs/operations/monitoring.md`](docs/operations/monitoring.md) |
| 运维排障手册 | [`docs/operations/runbook.md`](docs/operations/runbook.md) |
| 架构与开发规范 | [`docs/architecture/overview.md`](docs/architecture/overview.md) · [`CONTRIBUTING.md`](CONTRIBUTING.md) |
| 版本更新日记 | [`CHANGELOG.md`](CHANGELOG.md) · [安全说明](SECURITY.md) |

本地开发常用命令：

```bash
make ui-dev     # 启动 React 管理台开发服
make ui-build   # 构建并更新内嵌单文件管理台 (internal/admin/dist/index.html)
make test       # go test ./...
make test-race  # go test -race ./...
make build      # 构建本地二进制到 bin/
```

---

## 致谢

这个项目从最初的一个小代理一路走到 v0.8，离不开大家在真实生产环境里帮忙踩坑、抓包、提 Issue、交 PR。每一条反馈都在让它变得更稳、更顺手——**真心感谢每一位参与过的朋友！**

<details>
<summary>展开查看完整贡献与反馈鸣谢名单（按 Issue / PR 编号）</summary>

- [@dyed-fanxing](https://github.com/dyed-fanxing) · [#2](https://github.com/wnddd839/buddy-proxy/issues/2) ZCode 把 git status 写进上下文触发 11128 · [#4](https://github.com/wnddd839/buddy-proxy/issues/4) DeepSeek Flash 1M 上下文卡在约 70%
- [@carter003](https://github.com/carter003) · [#6](https://github.com/wnddd839/buddy-proxy/issues/6) 换号重试按剩余账号缩小上限 · [#8](https://github.com/wnddd839/buddy-proxy/issues/8) 同会话钉号与新会话按额度选号 · [#9](https://github.com/wnddd839/buddy-proxy/issues/9) 管理台版本号与用量明细 · [#10](https://github.com/wnddd839/buddy-proxy/issues/10) 用量表账号/模型筛选 · [#11](https://github.com/wnddd839/buddy-proxy/issues/11) hy3 缓存命中率对照 · [#27](https://github.com/wnddd839/buddy-proxy/pull/27) 复用上游 `X-Conversation-ID` · [#28](https://github.com/wnddd839/buddy-proxy/issues/28) Responses 工具回传 11148 · [#30](https://github.com/wnddd839/buddy-proxy/issues/30) flash 系合并连续分散 `tool_calls`
- [@tearslee](https://github.com/tearslee) · [#7](https://github.com/wnddd839/buddy-proxy/issues/7) DSH/Codex 缓存读取排查
- [@240xu](https://github.com/240xu) · [#12](https://github.com/wnddd839/buddy-proxy/pull/12) / [#35](https://github.com/wnddd839/buddy-proxy/issues/35) 模型目录探测矩阵与补齐 · [#48](https://github.com/wnddd839/buddy-proxy/pull/48) 波动监测 · [#49](https://github.com/wnddd839/buddy-proxy/pull/49) 批量导入 · [#50](https://github.com/wnddd839/buddy-proxy/pull/50) 账号固定出口 · [#51](https://github.com/wnddd839/buddy-proxy/pull/51) 隧道 SSE 连接复用 · [#52](https://github.com/wnddd839/buddy-proxy/pull/52) 请求体上限配置 · [#53](https://github.com/wnddd839/buddy-proxy/pull/53) keep-alive 空闲超时 · [#54](https://github.com/wnddd839/buddy-proxy/issues/54) 限流实测反馈 · [#57](https://github.com/wnddd839/buddy-proxy/pull/57) 按账号并发观测 · [#60](https://github.com/wnddd839/buddy-proxy/pull/60) 流式截断/停滞实测 · [#62](https://github.com/wnddd839/buddy-proxy/pull/62) 国际站 system 补全 · [#63](https://github.com/wnddd839/buddy-proxy/pull/63) 模型目录提速 · [#64](https://github.com/wnddd839/buddy-proxy/pull/64) OAuth 专用出口 · [#65](https://github.com/wnddd839/buddy-proxy/pull/65) 单号重连 · [#66](https://github.com/wnddd839/buddy-proxy/pull/66) 独立签到、模型路由与积分下限 · [#67](https://github.com/wnddd839/buddy-proxy/pull/67) 签到排班与推理字段 · [#69](https://github.com/wnddd839/buddy-proxy/pull/69) 工具 Schema 11129 修复 · [#70](https://github.com/wnddd839/buddy-proxy/pull/70) 额度冷却自愈与源区用尽路由 · [#71](https://github.com/wnddd839/buddy-proxy/pull/71) 冷却可观测性与模型可用数 · [#72](https://github.com/wnddd839/buddy-proxy/pull/72) System Prompt 开发者面板与两档看门狗 · [#73](https://github.com/wnddd839/buddy-proxy/pull/73) `strip` 精确剥离与 DSH 会话亲和 · [#74](https://github.com/wnddd839/buddy-proxy/pull/74) 本地实时扣减余额
- [@zeonseoi](https://github.com/zeonseoi) · [#13](https://github.com/wnddd839/buddy-proxy/issues/13) Qoder CN 同类封装建议 · [#19](https://github.com/wnddd839/buddy-proxy/issues/19) 流式 Markdown 标题渲染修复
- [@itaid](https://github.com/itaid) · [#14](https://github.com/wnddd839/buddy-proxy/issues/14) Claude Code system 指纹触发 11128
- [@kouekikin24](https://github.com/kouekikin24) · [#15](https://github.com/wnddd839/buddy-proxy/issues/15) 切区后旧钉号修复 · [#17](https://github.com/wnddd839/buddy-proxy/issues/17) 单进程双站并存 · [#18](https://github.com/wnddd839/buddy-proxy/issues/18) `/health` 与 `/readyz` 拆分 · [#20](https://github.com/wnddd839/buddy-proxy/issues/20) 模型前缀规范 · [#22](https://github.com/wnddd839/buddy-proxy/issues/22) 6004 冷却对齐重置时间 · [#23](https://github.com/wnddd839/buddy-proxy/issues/23) 禁号/删号后松钉 · [#24](https://github.com/wnddd839/buddy-proxy/issues/24) 多 Key 与号池过滤 · [#25](https://github.com/wnddd839/buddy-proxy/issues/25) 防止回写覆盖外部加号 · [#29](https://github.com/wnddd839/buddy-proxy/issues/29) / [#31](https://github.com/wnddd839/buddy-proxy/pull/31) 账号 Chat 测试对齐 · [#32](https://github.com/wnddd839/buddy-proxy/pull/32) 断电全零防损坏（fsync 原子写） · [#33](https://github.com/wnddd839/buddy-proxy/issues/33) 顶栏折行修复 · [#38](https://github.com/wnddd839/buddy-proxy/issues/38) 积分包到期明细 · [#39](https://github.com/wnddd839/buddy-proxy/issues/39) API Key 固定账号 · [#44](https://github.com/wnddd839/buddy-proxy/pull/44) 流重试保护 · [#45](https://github.com/wnddd839/buddy-proxy/issues/45) 输出 token 上限修复
- [@kkl31415926](https://github.com/kkl31415926) · [#16](https://github.com/wnddd839/buddy-proxy/issues/16) 流式 `tool_calls[].index` 补全
- [@kkblank](https://github.com/kkblank) · [#36](https://github.com/wnddd839/buddy-proxy/issues/36) Responses 工具截图结构化传输
- [@kylstwang](https://github.com/kylstwang) · [#37](https://github.com/wnddd839/buddy-proxy/issues/37) Linux ARM 预编译包需求
- [@ck629](https://github.com/ck629) · [#61](https://github.com/wnddd839/buddy-proxy/issues/61) 自更新下载断流重试

</details>

灵感与参考：[CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) · [Kiro-Go](https://github.com/Quorinex/Kiro-Go) · NewAPI 生态

---

## 免责声明与合规

**请花一分钟读完这一节**，这是本项目能够持续开源维护的前提。

### 这是什么
一个**自行托管、本地运行的协议转换工具**。它不提供任何模型服务，不售卖或托管任何账号，也不经过维护者中转任何流量——**你的每一次请求都只从你自己的机器直发给你所选的上游（CodeBuddy 或 WorkBuddy）**。

### 你的责任
使用本项目即表示你确认并同意：
1. **仅使用合法授权账号**：你只对本人合法拥有、且符合其服务条款的账号使用本工具。
2. **自行承担合规与账号风险**：本项目无法替你判断某个账号、地区或套餐的条款限制，也**不对账号限流、降权或封禁承担任何责任**。
3. **遵守当地法规与组织规定**：包括但不限于服务条款、地区法律法规及所在单位的安全要求。
4. **请勿绕过付费**：本项目无意也不应用于规避任何订阅费用或用量计费。

若你所在的环境不允许此类接入，**请不要使用本项目**。

### 我们不提供什么

| 不提供 | 说明 |
| :--- | :--- |
| 不提供账号 | 不售卖、不赠送、不代注册任何 CodeBuddy / WorkBuddy 账号或额度 |
| 不提供托管服务 | 没有公共实例，没有官方中转，不接触你的任何请求数据 |
| 不提供担保 | 软件按「原样」提供，不保证可用性或与上游的持续兼容 |
| 不承担损失 | 对使用造成的任何直接或间接损失（含账号风险、数据损失、业务中断）不承担责任 |

### 安全使用建议
- 默认监听 `127.0.0.1`；若通过 Nginx 等暴露到公网，务必保留 `CODEBUDDY_PROXY_API_KEY` 与管理台密码。
- **切勿把 `.env`、`proxy-accounts.json`、`proxies.json` 或任何 token / API Key 提交进仓库或分享给他人**。
- 管理台密码与 API Key 分开保管；定期备份账号池文件并妥善加密。
- 建议一进程独享一份 `proxy-accounts.json`；安全漏洞请勿直接开公开 Issue，参见 [SECURITY.md](SECURITY.md)。

---

## License

[BSD-3-Clause](LICENSE) · 开源分享，不含任何担保与责任。

<p align="center">
  <sub>Buddy Proxy 与 CodeBuddy、WorkBuddy 官方无关联、无隶属、无背书关系。「CodeBuddy」「WorkBuddy」为其各自持有者的商标，此处仅作技术兼容性描述之用。</sub>
</p>
