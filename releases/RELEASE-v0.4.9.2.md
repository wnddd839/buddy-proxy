## v0.4.9.2 · 2026-09-16 · 钉号切站自愈 · 流式 tool_calls index

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

- SITE 切换后，旧会话钉号遇 `site mismatch` 会自动 Forget 并重选当前区域账号（[#15](https://github.com/wnddd839/buddy-proxy/issues/15)）。
- 流式 `delta.tool_calls[].index` 即使为 `0` 也会输出，修复 Android Studio / OpenAI Java SDK（[#16](https://github.com/wnddd839/buddy-proxy/issues/16)）。
- 显式 `"stream_options":{"include_usage":false}` 时不再推送空 `choices` 的 usage 收尾 chunk。

### 感谢

- [@kouekikin24](https://github.com/kouekikin24) · [#15](https://github.com/wnddd839/buddy-proxy/issues/15)
- [@kkl31415926](https://github.com/kkl31415926) · [#16](https://github.com/wnddd839/buddy-proxy/issues/16)

### 升级

覆盖旧二进制后重启。配置不用改。

完整说明见 [`CHANGELOG.md`](https://github.com/wnddd839/buddy-proxy/blob/main/CHANGELOG.md)。
