## v0.5 · 2026-09-20 · Responses API · 同会话复用上游会话 ID

### 下载哪个文件？

| 你的系统 | 下载 |
|----------|------|
| **Windows 64 位** | `codebuddy-proxy-windows-x64.exe` |
| Linux x64 | `codebuddy-proxy-linux-amd64` |
| macOS Apple 芯片 | `codebuddy-proxy-darwin-arm64` |
| macOS Intel | `codebuddy-proxy-darwin-amd64` |

校验：`SHA256SUMS.txt`

---

### 改了什么

- Codex CLI 可直连：`POST /v1/responses` 已按 OpenAI Responses API 做协议翻译（流式命名 SSE + 非流式 `response` 对象），打同一套号池。
- 同会话复用上游 `X-Conversation-ID`，稳住 prompt cache 命中（[#27](https://github.com/wnddd839/buddy-proxy/pull/27)）。`request/message ID` 仍逐请求随机。
- 管理台接入页可复制 Responses URL。点「刷新状态」会向上游拉当前号池最新 Credits；`background` / 内置工具 / 服务端 `store` 仍不支持。

### 感谢

- [@carter003](https://github.com/carter003) · [#27](https://github.com/wnddd839/buddy-proxy/pull/27)

### 升级

覆盖旧二进制后重启。账号池不用迁移。

Codex CLI：

```toml
model_provider = "buddy-proxy"
model = "auto"

[model_providers.buddy-proxy]
name = "Buddy Proxy"
base_url = "http://127.0.0.1:32126/v1"
env_key = "CODEBUDDY_PROXY_API_KEY"
wire_api = "responses"
```

完整说明见 [`CHANGELOG.md`](https://github.com/wnddd839/buddy-proxy/blob/main/CHANGELOG.md)。
