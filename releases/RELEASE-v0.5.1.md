## v0.5.1 · 2026-09-21 · Responses 工具回传配对 11148

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

- Codex / Responses 第二轮回传工具结果时，不再把 `assistant.tool_calls` 滤掉，上游不再报 11148（[#28](https://github.com/wnddd839/buddy-proxy/issues/28)）。
- `tool_calls` 的 `[]map[string]any` 与 `[]any` 会先归一再判断空消息。

### 感谢

- [@carter003](https://github.com/carter003) · [#28](https://github.com/wnddd839/buddy-proxy/issues/28)

### 升级

覆盖旧二进制后重启。账号池不用迁移。v0.5 的 Codex 配置不用改。

完整说明见 [`CHANGELOG.md`](https://github.com/wnddd839/buddy-proxy/blob/main/CHANGELOG.md)。
