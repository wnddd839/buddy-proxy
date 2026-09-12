<p align="center">
  <img src="docs/logo.svg" width="88" height="88" alt="Buddy Proxy" />
</p>

<h1 align="center">Buddy Proxy</h1>

<p align="center">
  <strong>把 CodeBuddy / WorkBuddy 账号，变成任何 OpenAI 客户端都能直连的 <code>/v1</code> 渠道。</strong>
</p>

<p align="center">
  <a href="https://wnddd839.github.io/buddy-proxy/"><img src="https://img.shields.io/badge/Product-Page-1C1C1C?style=flat-square" alt="Product Page" /></a>
  <a href="https://github.com/wnddd839/buddy-proxy/blob/main/LICENSE"><img src="https://img.shields.io/badge/License-BSD--3--Clause-1C1C1C?style=flat-square" alt="License" /></a>
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-%E2%89%A51.26-1C1C1C?style=flat-square&logo=go&logoColor=white" alt="Go" /></a>
  <img src="https://img.shields.io/badge/Transport-protocol__direct-1C1C1C?style=flat-square" alt="protocol_direct" />
</p>

<p align="center">
  <a href="https://wnddd839.github.io/buddy-proxy/">产品页</a> ·
  <a href="#快速开始">快速开始</a> ·
  <a href="#接入客户端">接入</a> ·
  <a href="#文档">文档</a> ·
  <a href="#免责声明与合规">免责声明</a>
</p>

---

感谢这些同学用 issue 把真实问题送上门，按编号：

- [@dyed-fanxing](https://github.com/dyed-fanxing) · [#2](https://github.com/wnddd839/buddy-proxy/issues/2) ZCode 把 git status 写进上下文触发 11128 · [#4](https://github.com/wnddd839/buddy-proxy/issues/4) DeepSeek Flash 1M 上下文卡在约 70%
- [@carter003](https://github.com/carter003) · [#6](https://github.com/wnddd839/buddy-proxy/issues/6) 换号重试按剩余账号缩小上限、提前终止 · [#8](https://github.com/wnddd839/buddy-proxy/issues/8) 同会话应钉在一个账号，新会话再按额度选号
- [@tearslee](https://github.com/tearslee) · [#7](https://github.com/wnddd839/buddy-proxy/issues/7) 经代理走 DSH/Codex 时缓存读取一直是 0

---

## 一件事

你有腾讯 CodeBuddy 或 WorkBuddy 账号。你有一堆只认 OpenAI `/v1` 格式的工具——NewAPI、ZCode、Sub2API、各类 SDK 和客户端。

**Buddy Proxy 是中间那一层协议翻译器。**

用你自己的账号（OAuth 登录）直连上游，对外暴露标准 OpenAI 接口。管理台可以切国内 / 国际，也可以切 CodeBuddy / WorkBuddy——同一套 token，产品和站点正交。不需要改客户端，不需要 `codebuddy --serve`，不需要碰任何浏览器插件。

```text
你的客户端  ──►  Buddy Proxy  ──►  CodeBuddy 或 WorkBuddy
(OpenAI格式)     (协议翻译/账号池)     (protocol_direct)
```

一个 Go 写的单文件二进制，跑在你自己的机器上。二进制名仍是 `codebuddy-proxy`。

---

## 它做了什么

| 能力 | 说明 |
| :--- | :--- |
| **协议直连** | OAuth 登录后直连上游，不依赖 `codebuddy --serve` 等本地中间进程 |
| **标准 OpenAI 形状** | `GET /v1/models` · `POST /v1/chat/completions`，流式与非流式都支持 |
| **多账号调度** | 同会话钉在同一账号；新会话按额度快照选最大（缺快照或超过 5 分钟才探活）；失败换号前再探活拿最大；凭据以 `0600` 权限落盘 |
| **真实余额** | 管理台直读官网 Credits，显示「剩余 / 总额」 |
| **国内 / 国际** | 一键切换号池；**端点以账号自身区域为准**，不会把国内号打到海外 |
| **CodeBuddy / WorkBuddy** | 一键切产品：CodeBuddy 走 CLI 头，WorkBuddy 走 IDE 头；模型目录随之切换 |
| **模型列表** | 走协议 `/v3/config`，60 秒缓存，可强制刷新 |
| **Token 用量透传** | 流式收尾补 usage chunk，含缓存命中统计（缓存字段兼容多上游别名） |
| **开箱即用** | 预编译二进制，无运行时依赖；首次启动自动生成 API Key |

---

## 快速开始

### 方式 A：下载即用（推荐）

从 [GitHub Releases](https://github.com/wnddd839/buddy-proxy/releases/latest) 下载对应平台文件，直接运行。

| 系统 | 文件 |
|------|------|
| **Windows 64 位** | `codebuddy-proxy-windows-x64.exe` |
| Linux 64 位 | `codebuddy-proxy-linux-amd64` |
| macOS Apple 芯片 | `codebuddy-proxy-darwin-arm64` |
| macOS Intel | `codebuddy-proxy-darwin-amd64` |

```powershell
# Windows
.\codebuddy-proxy-windows-x64.exe
```

```bash
# Linux / macOS
chmod +x ./codebuddy-proxy-linux-amd64
./codebuddy-proxy-linux-amd64
```

首次启动会自动生成 API Key 并写入同目录 `.env`，日志里会打印出来。

### 方式 B：从源码

```bash
git clone https://github.com/wnddd839/buddy-proxy.git
cd buddy-proxy
go run ./cmd/codebuddy-proxy
```

### 三个入口

| | |
| :--- | :--- |
| API | `http://127.0.0.1:32126/v1` |
| 管理台 | `http://127.0.0.1:32126/direct-admin/` |
| Health | `http://127.0.0.1:32126/health` |

### 三步跑起来

1. 打开管理台 → 选「国内」或「国际」、选「CodeBuddy」或「WorkBuddy」→ 点「开始 OAuth」，浏览器完成授权
2. 回管理台点「检查登录」，账号进入账号池
3. 复制页面上的 **Base URL + API Key**，填进你的客户端

管理台密码留空即为免密（本地推荐）。`/v1` 的 API Key 建议保持开启。

> 管理台「生成 API Key」会写入 `.env` 并**立即生效**——旧 Key 当场失效，客户端必须同步更换。

---

## 接入客户端

任何 OpenAI 兼容客户端都行：

```text
Base URL   http://<host>:32126/v1
API Key    管理台复制的那个
Model      auto  或 GET /v1/models 返回的 id
```

```bash
curl http://127.0.0.1:32126/v1/chat/completions \
  -H "Authorization: Bearer $CODEBUDDY_PROXY_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"auto","stream":true,"messages":[{"role":"user","content":"你好"}]}'
```

模型 `id` 不带 `codebuddy/` 前缀，但请求时 `codebuddy/auto` 和 `auto` 都接受。

> 只提供列表接口，**不支持** `GET /v1/models/{id}` 单模型查询（返回 404）。请在客户端侧从列表中匹配。

国内 / 国际、CodeBuddy / WorkBuddy 切换只需改 `.env`（或在管理台点切换）：

```env
# 站点
CODEBUDDY_SITE=domestic          # 或 global
CODEBUDDY_INTERNET_ENVIRONMENT=internal   # 国际用 public

# 产品（与站点正交；同一套 token）
CODEBUDDY_PRODUCT=codebuddy      # 或 workbuddy
```

完整变量见 [配置参考](docs/guides/configuration.md)。

---

## 文档

| 资源 | 链接 |
| :--- | :--- |
| 产品页 | [wnddd839.github.io/buddy-proxy](https://wnddd839.github.io/buddy-proxy/) |
| 文档索引 | [`docs/README.md`](docs/README.md) |
| 快速开始 | [`guides/getting-started.md`](docs/guides/getting-started.md) |
| 配置参考 | [`guides/configuration.md`](docs/guides/configuration.md) |
| HTTP API | [`api/http.md`](docs/api/http.md) |
| 架构说明 | [`architecture/overview.md`](docs/architecture/overview.md) |
| 运维排障 | [`operations/runbook.md`](docs/operations/runbook.md) |
| 预编译包 | [GitHub Releases](https://github.com/wnddd839/buddy-proxy/releases/latest) |
| 更新日记 | [`CHANGELOG.md`](CHANGELOG.md) · [安全说明](SECURITY.md) |

开发命令（仓库根目录）：

```bash
make test      # go test ./...
make build     # 产出 bin/
make release   # 四平台交叉编译 + SHA256SUMS.txt
```

另有 `make fmt` / `vet` / `test-race` / `check`。

---

## 免责声明与合规

**请用一分钟读完这一节。** 它是本项目持续开源的前提。

### 这是什么

一个**自行托管、本地运行的协议转换工具**。它不提供任何模型服务，不代理任何第三方 API，不托管任何账号，不中转任何流量到本项目维护者——**你的请求只从你自己的机器发往你所选的上游（CodeBuddy 或 WorkBuddy）**。

### 你的责任

使用本项目即表示你确认并同意：

1. **你只对拥有合法授权、且符合其服务条款的账号使用本工具。** 账号是否允许此类接入，由你与该服务的协议决定。
2. **遵守服务条款是你的责任。** 本项目无法代你判断某个账号、某个地区、某类套餐是否允许此类用法，也**不为你的账号被限制、降权、封禁承担任何责任**。
3. **合规自查在你这边。** 包括但不限于：服务条款、地区法律法规、单位或组织的内部规定。
4. **不要绕过付费。** 本项目无意也不应被用于规避订阅费用或用量计费。

若你所在的环境不允许此类接入，**请不要使用本项目**。

### 我们不提供什么

| 不提供 | 说明 |
| :--- | :--- |
| 不提供账号 | 不售卖、不赠送、不代注册任何 CodeBuddy / WorkBuddy 账号或额度 |
| 不提供托管服务 | 没有公共实例，没有官方部署，不接收你的流量 |
| 不提供担保 | 软件按「原样」提供，不保证可用性、不保证与上游的持续兼容 |
| 不承担损失 | 对使用造成的任何直接或间接损失（含账号风险、数据损失、业务中断）不承担责任 |

### 安全使用建议

- 默认绑定 `127.0.0.1`，需要局域网访问时自行评估暴露面
- 暴露 `/v1` 时务必设置 `CODEBUDDY_PROXY_API_KEY`
- **不要把 `.env`、账号 JSON、token、API Key 提交进仓库或分享给他人**
- 管理台密码与 API Key 分开管理；本项目已移除 URL query 传密方式
- 定期备份账号池 JSON，但注意其中包含凭据

漏洞报告请勿开公开 issue，见 [SECURITY.md](SECURITY.md)。

---

## License

[BSD-3-Clause](LICENSE) · 开源分享，不含任何担保与责任。

灵感与参考：[CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) · [Kiro-Go](https://github.com/Quorinex/Kiro-Go) · NewAPI 生态

---

<p align="center">
  <sub>Buddy Proxy 与 CodeBuddy、WorkBuddy 官方无关联、无隶属、无背书关系。「CodeBuddy」「WorkBuddy」为其各自持有者的商标，此处仅作技术兼容性描述之用。</sub>
</p>
