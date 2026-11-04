# Pre-built binaries

二进制**不进 Git**。请从 GitHub Releases 下载：

- 最新版：https://github.com/wnddd839/buddy-proxy/releases/latest

| 平台 | 资源名 |
|------|--------|
| **Windows 64 位** | `codebuddy-proxy-windows-x64.exe`（推荐） |
| Linux 64 位 | `codebuddy-proxy-linux-amd64` |
| Linux ARM64 | `codebuddy-proxy-linux-arm64` |
| Linux ARMv7（32 位） | `codebuddy-proxy-linux-armv7` |
| Linux ARMv6（32 位） | `codebuddy-proxy-linux-armv6` |
| macOS Apple 芯片 (M1/M2/M3) | `codebuddy-proxy-darwin-arm64` |
| macOS Intel | `codebuddy-proxy-darwin-amd64` |

> **Windows 用户请勿下载 `darwin-arm64` / `darwin-amd64`**，那是 macOS 专用，在 Windows 上会报错。

校验文件随 Release 附带：`SHA256SUMS.txt`

本目录只保留说明与模板：

- `README.md`（本文件）
- `.env.example`

本地从源码打包：

```bash
make release
```

Windows PowerShell：

```powershell
.\scripts\release.ps1 -Version v0.7.2
```

ARM 构建不依赖 CGO。请先通过 `uname -m` 确认设备架构：`aarch64` 选择 arm64，`armv7l` 选择 armv7，ARMv6 选择 armv6。MIPS 路由器不能运行 ARM 二进制。发行包已完成交叉构建，但仍需在目标设备确认系统与指令集兼容性。

## Windows 快速开始

1. 从 [Releases](https://github.com/wnddd839/buddy-proxy/releases/latest) 下载 **`codebuddy-proxy-windows-x64.exe`**
2. 直接运行（会自动读取/创建同目录 `.env`）

```powershell
.\codebuddy-proxy-windows-x64.exe
```

3. 打开 Admin：`http://127.0.0.1:32126/direct-admin/`
4. 复制页面上的 Base URL + API Key 填到 ZCode / NewAPI

> 首次启动若没有 Key，程序会自动生成 `cbp_...` 并写入 `.env`。  
> 管理台「生成 API Key」也会写入 `.env`；旧 Key 立即失效，客户端必须同步更换。  
> 管理台密码可留空（免密）；`/v1` 仍建议开着 API Key。

## Linux / macOS

```bash
chmod +x ./codebuddy-proxy-linux-amd64   # 或 darwin 对应文件
./codebuddy-proxy-linux-amd64
```

## 环境变量（也可写入 `.env`）

| 变量 | 说明 |
|------|------|
| `CODEBUDDY_PROXY_ADMIN_PASSWORD` | 管理后台密码；**留空=免密**（本地推荐）。API Key 仍可单独开启 |
| `CODEBUDDY_PROXY_API_KEY` | 客户端主 Key |
| `CODEBUDDY_PROXY_API_KEYS` | 可选。多 Key 绑区域或账号，例如 `cbp_aaa:global,cbp_fixed:account=<id>` |
| `CODEBUDDY_PROXY_PORT` | 端口，默认 `32126` |
| `CODEBUDDY_PROXY_MAX_BODY_MB` | 公开推理接口 JSON 上限（MiB），默认 `64` |
| `CODEBUDDY_PROXY_SERVER_IDLE_TIMEOUT_MS` | HTTP keep-alive 空闲超时（毫秒），默认 `120000`，不限制活跃 SSE |
| `CODEBUDDY_SITE` | `domestic` 或 `global` |
| `CODEBUDDY_PRODUCT` | `codebuddy`（默认）或 `workbuddy` |
| `CODEBUDDY_PROXY_ACCOUNTS_PATH` | 账号池 JSON 路径 |

完整说明见 [`../docs/guides/getting-started.md`](../docs/guides/getting-started.md).
