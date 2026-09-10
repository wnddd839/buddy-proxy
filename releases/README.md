# Pre-built binaries

二进制**不进 Git**。请从 GitHub Releases 下载：

- 最新版：https://github.com/wnddd839/buddy-proxy/releases/latest

| 平台 | 资源名 |
|------|--------|
| **Windows 64 位** | `codebuddy-proxy-windows-x64.exe`（推荐） |
| Linux 64 位 | `codebuddy-proxy-linux-amd64` |
| macOS Apple 芯片 (M1/M2/M3) | `codebuddy-proxy-darwin-arm64` |
| macOS Intel | `codebuddy-proxy-darwin-amd64` |

> **Windows 用户请勿下载 `darwin-arm64` / `darwin-amd64`**，那是 macOS 专用，在 Windows 上会报错。

校验文件随 Release 附带：`SHA256SUMS.txt`

本目录只保留说明与模板：

- `README.md`（本文件）
- `.env.example`

本地从源码打包：

```bash
cd go-codebuddy
make release
```

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
| `CODEBUDDY_PROXY_API_KEY` | 客户端 API Key |
| `CODEBUDDY_PROXY_PORT` | 端口，默认 `32126` |
| `CODEBUDDY_SITE` | `domestic` 或 `global` |
| `CODEBUDDY_PROXY_ACCOUNTS_PATH` | 账号池 JSON 路径 |

完整说明见 [`../docs/guides/getting-started.md`](../docs/guides/getting-started.md).
