## v0.4.9.1 · 2026-09-14 · 下游 system 折叠

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

- 客户端 `system` / `developer` 折进 user，避免上游 11128。不按 ZCode / Claude Code 逐家适配。

### 感谢

- [@itaid](https://github.com/itaid) · [#14](https://github.com/wnddd839/buddy-proxy/issues/14)
- [@zeonseoi](https://github.com/zeonseoi) · [#13](https://github.com/wnddd839/buddy-proxy/issues/13)

### 升级

覆盖旧二进制后重启。配置不用改。

完整说明见 [`CHANGELOG.md`](https://github.com/wnddd839/buddy-proxy/blob/main/CHANGELOG.md)。
