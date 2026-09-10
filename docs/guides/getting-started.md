# Getting Started

## 环境要求

- Go **1.26+**（`go.mod` 声明 `go 1.26.4`）
- 可选：已有 CodeBuddy 账号（用于 OAuth）

## 方式 A：预编译二进制（推荐）

从 [GitHub Releases](https://github.com/wnddd839/buddy-proxy/releases/latest) 下载对应平台文件，直接运行。
程序会自动读取（必要时创建）同目录 `.env`。

```powershell
# Windows
.\codebuddy-proxy-windows-x64.exe
```

```bash
# Linux / macOS
chmod +x ./codebuddy-proxy-linux-amd64
./codebuddy-proxy-linux-amd64
```

首次启动若没有 API Key，会生成 `cbp_...` 并写入 `.env`，日志里会打印。

## 方式 B：从源码

```bash
cd go-codebuddy
cp .env.example .env   # 可选
go run ./cmd/codebuddy-proxy
# 或
make build && ./bin/codebuddy-proxy
```

## 三个入口

```text
API      http://127.0.0.1:32126/v1
Admin    http://127.0.0.1:32126/direct-admin/
Health   http://127.0.0.1:32126/health
```

## 首次登录

1. 打开 `http://127.0.0.1:32126/direct-admin/`
2. 若设置了 `CODEBUDDY_PROXY_ADMIN_PASSWORD`，用 Basic Auth 登录（用户名 `admin`）
3. 选择 `domestic` 或 `global`，点「开始 OAuth」
4. 在浏览器完成授权，回到管理台点「检查登录」
5. 账号写入 `CODEBUDDY_PROXY_ACCOUNTS_PATH`

## 接入客户端

```text
Base URL   http://<host>:32126/v1
API Key    与 CODEBUDDY_PROXY_API_KEY 相同（管理台可复制）
Model      auto  或 GET /v1/models 返回的 id
```

## 验证

```bash
curl http://127.0.0.1:32126/health

curl http://127.0.0.1:32126/v1/models \
  -H "Authorization: Bearer $CODEBUDDY_PROXY_API_KEY"

curl http://127.0.0.1:32126/v1/chat/completions \
  -H "Authorization: Bearer $CODEBUDDY_PROXY_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"auto","messages":[{"role":"user","content":"你好"}]}'
```

> `GET /v1/models/{id}` 不支持，会返回 404。请拉列表后在客户端侧匹配 id。

## 配置速查

最小配置（API Key 留空会自动生成）：

```env
CODEBUDDY_PROXY_HOST=127.0.0.1
CODEBUDDY_PROXY_PORT=32126
CODEBUDDY_PROXY_REQUIRE_API_KEY=true
CODEBUDDY_PROXY_API_KEY=
CODEBUDDY_PROXY_ADMIN_PASSWORD=
CODEBUDDY_SITE=domestic
CODEBUDDY_INTERNET_ENVIRONMENT=internal
```

完整变量说明见 [configuration.md](configuration.md)。

## 常见问题

| 现象 | 处理 |
|------|------|
| `401` on `/v1` | 检查 `CODEBUDDY_PROXY_API_KEY` 与 `REQUIRE_API_KEY` |
| `no credentials` | 先完成 OAuth 登录 |
| 管理台打不开 / 一直弹登录 | 确认 admin 密码；留空则免密 |
| 国内账号打到海外 | 端点以**账号 site** 为准；检查账号 `site` 字段 |
| 模型列表只有 `auto` | 未登录或上游 `/v3/config` 为空；点管理台「刷新模型」 |

更多排障见 [../operations/runbook.md](../operations/runbook.md)。
