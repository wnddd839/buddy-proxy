## v0.5.3 · 2026-09-23 · flash 11148 合并分散 tool_calls · 顶栏不再逐字折行

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

- flash 系不再因为「连续空 `assistant.tool_calls` + 后置集中 tool 结果」打回 11148（[#30](https://github.com/wnddd839/buddy-proxy/issues/30)）。交错 A/T、带正文的 assistant、孤儿 tool 都不动。
- 切号池后顶栏状态条不再逐字折行（[#33](https://github.com/wnddd839/buddy-proxy/issues/33)）。顶栏版本旁应看到 `ui 2026.09.23-status-pills`。

### 感谢

- [@carter003](https://github.com/carter003) · [#30](https://github.com/wnddd839/buddy-proxy/issues/30)
- [@kouekikin24](https://github.com/kouekikin24) · [#33](https://github.com/wnddd839/buddy-proxy/issues/33)

### 升级

覆盖旧二进制后重启。账号池不用迁移。

完整说明见 [`CHANGELOG.md`](https://github.com/wnddd839/buddy-proxy/blob/main/CHANGELOG.md)。
